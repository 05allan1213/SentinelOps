#!/usr/bin/env bash
# 本地与 Pull Request 共用的硬门禁入口。
set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd -P)"
cd "${REPO_ROOT}"

LANE="all"
while (($# > 0)); do
  case "$1" in
    --lane)
      (($# >= 2)) || { echo "ERROR: --lane requires backend, frontend, contracts or all" >&2; exit 2; }
      LANE="$2"
      shift 2
      ;;
    --help)
      echo "usage: scripts/ci/pr.sh [--lane backend|frontend|contracts|all]"
      exit 0
      ;;
    *)
      echo "ERROR: unknown argument: $1" >&2
      exit 2
      ;;
  esac
done

case "${LANE}" in
  backend|frontend|contracts|all) ;;
  *) echo "ERROR: unsupported lane ${LANE}" >&2; exit 2 ;;
esac

fail() {
  echo "FAIL: $1" >&2
  exit 1
}

run_gofmt_gate() {
  local -a go_files
  mapfile -d '' -t go_files < <(git ls-files -z -- '*.go' ':!vendor/**')
  if ((${#go_files[@]} == 0)); then
    return 0
  fi
  local output
  output="$(gofmt -l "${go_files[@]}")"
  if [[ -n "${output}" ]]; then
    echo "${output}" >&2
    fail "gofmt reported unformatted Go files"
  fi
}

require_staticcheck() {
  local expected="${STATICCHECK_VERSION:-2026.2.1}"
  command -v staticcheck >/dev/null 2>&1 || fail "staticcheck ${expected} is required"
  local version
  version="$(staticcheck -version 2>/dev/null || true)"
  [[ "${version}" == *"${expected}"* ]] || fail "staticcheck version is not pinned to ${expected}"
}

require_govulncheck() {
  command -v govulncheck >/dev/null 2>&1 || fail "govulncheck is required"
  local binary_version expected
  expected="${GOVULNCHECK_MODULE_VERSION:-v1.7.0}"
  binary_version="$(go version -m "$(command -v govulncheck)" 2>/dev/null || true)"
  [[ "${binary_version}" == *"golang.org/x/vuln ${expected}"* ]] || fail "govulncheck module version is not pinned to ${expected}"
}

run_backend_gate() {
  run_gofmt_gate
  go mod tidy -diff
  go vet ./...
  require_staticcheck
  staticcheck ./...
  require_govulncheck
  govulncheck ./...
  [[ -n "${SENTINELOPS_TEST_DSN:-}" ]] || fail "SENTINELOPS_TEST_DSN is required for the race gate"
  go test -race ./...
}

run_contract_gate() {
  go test ./internal/testutil/agent ./internal/ai/tools/mcp ./internal/ai/agent/skill_pipeline ./internal/ai/policy -count=1
  go test ./internal/ai/runtime ./internal/ai/effects ./internal/ai/workflow \
    -run '^TestFaultInjectionRepresentative$' -count=1
}

run_frontend_gate() {
  command -v npm >/dev/null 2>&1 || fail "npm is required for the frontend gate"
  [[ -f web/package.json && -f web/package-lock.json ]] || fail "web package manifests are missing"
  npm ci --prefix web
  npm run lint --prefix web
  npm run build --prefix web
}

case "${LANE}" in
  backend) run_backend_gate ;;
  contracts) run_contract_gate ;;
  frontend) run_frontend_gate ;;
  all)
    run_backend_gate
    run_contract_gate
    run_frontend_gate
    ;;
esac

echo "PASS: PR quality gates (${LANE})"
