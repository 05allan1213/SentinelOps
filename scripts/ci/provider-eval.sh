#!/usr/bin/env bash
# 定时/手动真实供应商 preflight 与生产 Runtime Eval；缺少 Secret 时只报告 NOT RUN。
set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd -P)"
cd "${REPO_ROOT}"

ARTIFACT_DIR="${SENTINELOPS_P41_ARTIFACT_DIR:-${REPO_ROOT}/.artifacts/p41/provider-eval}"
TIMEOUT="${SENTINELOPS_PROVIDER_EVAL_TIMEOUT:-120m}"
REPEAT="${SENTINELOPS_PROVIDER_EVAL_REPEAT:-3}"

while (($# > 0)); do
  case "$1" in
    --artifact-dir)
      (($# >= 2)) || { echo "ERROR: --artifact-dir requires a path" >&2; exit 2; }
      ARTIFACT_DIR="$2"
      shift 2
      ;;
    --timeout)
      (($# >= 2)) || { echo "ERROR: --timeout requires a Go duration" >&2; exit 2; }
      TIMEOUT="$2"
      shift 2
      ;;
    --repeat)
      (($# >= 2)) || { echo "ERROR: --repeat requires a positive integer" >&2; exit 2; }
      REPEAT="$2"
      shift 2
      ;;
    --help)
      echo "usage: scripts/ci/provider-eval.sh [--artifact-dir PATH] [--timeout DURATION] [--repeat N]"
      exit 0
      ;;
    *)
      echo "ERROR: unknown argument: $1" >&2
      exit 2
      ;;
  esac
done

[[ "${REPEAT}" =~ ^[1-9][0-9]*$ ]] || { echo "ERROR: --repeat must be a positive integer" >&2; exit 2; }
mkdir -p "${ARTIFACT_DIR}"

TMP_WORK="$(mktemp -d "${TMPDIR:-/tmp}/sentinelops-provider-eval.XXXXXX")"
cleanup() {
  local code=$?
  trap - EXIT INT TERM
  case "${TMP_WORK}" in
    "${TMPDIR:-/tmp}"/sentinelops-provider-eval.*) rm -rf -- "${TMP_WORK}" ;;
    *) echo "WARN: refused unexpected temporary path" >&2 ;;
  esac
  exit "${code}"
}
trap cleanup EXIT INT TERM

write_status() {
  local status="$1" stage="$2" reason="$3"
  python3 - "${ARTIFACT_DIR}/status.json" "${status}" "${stage}" "${reason}" <<'PY'
import datetime
import json
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
payload = {
    "schema": "sentinelops/provider-eval-status/v1",
    "status": sys.argv[2],
    "stage": sys.argv[3],
    "reason": sys.argv[4],
    "completed_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
}
path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n")
PY
}

redact_file() {
  local input="$1" output="$2"
  go run ./scripts/ci/redact.go <"${input}" >"${output}"
}

missing=()
for name in SENTINELOPS_MODEL_API_KEY SENTINELOPS_EVAL_DSN SENTINELOPS_EVAL_BASE_URL; do
  if [[ -z "${!name:-}" ]]; then
    missing+=("${name}")
  fi
done
if ((${#missing[@]} > 0)); then
  reason="missing required references: $(IFS=,; echo "${missing[*]}")"
  write_status "NOT RUN" "preflight" "${reason}"
  echo "NOT RUN: provider Eval prerequisites are unavailable"
  exit 0
fi

run_preflight() {
  local name="$1" package="$2" test_name="$3"
  local raw="${TMP_WORK}/${name}.raw.log"
  if ! SENTINELOPS_ONLINE_TEST=1 go test "${package}" -run "^${test_name}$" -count=1 >"${raw}" 2>&1; then
    redact_file "${raw}" "${ARTIFACT_DIR}/preflight-${name}.log"
    write_status "FAIL" "preflight-${name}" "online provider preflight failed"
    echo "FAIL: provider preflight ${name}; see redacted artifact" >&2
    return 1
  fi
  redact_file "${raw}" "${ARTIFACT_DIR}/preflight-${name}.log"
}

run_preflight text-generation ./internal/ai/models TestProviderTextGenerationOnline
run_preflight tool-calling ./internal/ai/models TestProviderToolCallingOnline
run_preflight embedding ./internal/ai/embedder TestProviderEmbeddingOnline
run_preflight rerank ./internal/ai/rerank TestProviderRerankOnline

raw_report="${TMP_WORK}/runtime-eval.raw.json"
raw_log="${TMP_WORK}/runtime-eval.raw.log"
eval_args=(
  run
  --cases manifest/eval/cases
  --base-url "${SENTINELOPS_EVAL_BASE_URL}"
  --dsn-ref env:SENTINELOPS_EVAL_DSN
  --timeout "${TIMEOUT}"
  --repeat "${REPEAT}"
)
if [[ -n "${SENTINELOPS_EVAL_AUTHORIZATION:-}" ]]; then
  eval_args+=(--authorization-ref env:SENTINELOPS_EVAL_AUTHORIZATION)
fi

set +e
go run ./cmd/agenteval "${eval_args[@]}" >"${raw_report}" 2>"${raw_log}"
eval_code=$?
set -e
redact_file "${raw_report}" "${ARTIFACT_DIR}/runtime-eval.json"
redact_file "${raw_log}" "${ARTIFACT_DIR}/runtime-eval.log"
if ((eval_code != 0)); then
  write_status "FAIL" "runtime-eval" "production Runtime Eval failed"
  echo "FAIL: production Runtime Eval; see redacted artifact" >&2
  exit "${eval_code}"
fi

baseline_status="NOT RUN"
if [[ -f manifest/eval/baselines/approved.yaml ]]; then
  raw_compare="${TMP_WORK}/baseline-compare.raw.json"
  raw_compare_log="${TMP_WORK}/baseline-compare.raw.log"
  set +e
  go run ./cmd/agenteval compare \
    --baseline manifest/eval/baselines/approved.yaml \
    --report "${ARTIFACT_DIR}/runtime-eval.json" \
    >"${raw_compare}" 2>"${raw_compare_log}"
  compare_code=$?
  set -e
  redact_file "${raw_compare}" "${ARTIFACT_DIR}/baseline-compare.json"
  redact_file "${raw_compare_log}" "${ARTIFACT_DIR}/baseline-compare.log"
  if ((compare_code != 0)); then
    write_status "FAIL" "baseline-compare" "approved baseline comparison failed"
    echo "FAIL: approved baseline comparison; see redacted artifact" >&2
    exit "${compare_code}"
  fi
  baseline_status="PASS"
fi

write_status "PASS" "runtime-eval" "four provider preflights and production Runtime Eval passed; baseline comparison ${baseline_status}"
echo "PASS: provider preflights and production Runtime Eval; baseline comparison ${baseline_status}"
