#!/usr/bin/env bash
# 在唯一 sentinelops-e2e Compose project 中执行 P41 集成、故障代表 Case 与 P38 浏览器链。
set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd -P)"
cd "${REPO_ROOT}"

PROJECT="sentinelops-e2e"
HTTP_PORT="${SENTINELOPS_E2E_HTTP_PORT:-18080}"
MYSQL_PORT="${SENTINELOPS_E2E_MYSQL_PORT:-13306}"
ARTIFACT_DIR="${SENTINELOPS_P41_ARTIFACT_DIR:-${REPO_ROOT}/.artifacts/p41/integration}"
KEEP_STACK=0
INSTALL_BROWSER=0
GOOSE_CONTAINER=""

while (($# > 0)); do
  case "$1" in
    --project)
      (($# >= 2)) || { echo "ERROR: --project requires sentinelops-e2e" >&2; exit 2; }
      [[ "$2" == "$PROJECT" ]] || { echo "ERROR: refusing project other than ${PROJECT}" >&2; exit 2; }
      shift 2
      ;;
    --http-port)
      (($# >= 2)) || { echo "ERROR: --http-port requires a value" >&2; exit 2; }
      HTTP_PORT="$2"
      shift 2
      ;;
    --artifact-dir)
      (($# >= 2)) || { echo "ERROR: --artifact-dir requires a path" >&2; exit 2; }
      ARTIFACT_DIR="$2"
      shift 2
      ;;
    --install-browser)
      INSTALL_BROWSER=1
      shift
      ;;
    --keep-stack)
      KEEP_STACK=1
      shift
      ;;
    --help)
      echo "usage: scripts/ci/integration.sh [--project sentinelops-e2e] [--http-port PORT] [--artifact-dir PATH] [--install-browser] [--keep-stack]"
      exit 0
      ;;
    *)
      echo "ERROR: unknown argument: $1" >&2
      exit 2
      ;;
  esac
done

if ! [[ "${HTTP_PORT}" =~ ^[0-9]+$ ]] || ((HTTP_PORT < 1024 || HTTP_PORT > 65535)); then
  echo "ERROR: --http-port must be between 1024 and 65535" >&2
  exit 2
fi
if ! [[ "${MYSQL_PORT}" =~ ^[0-9]+$ ]] || ((MYSQL_PORT < 1024 || MYSQL_PORT > 65535)); then
  echo "ERROR: SENTINELOPS_E2E_MYSQL_PORT must be between 1024 and 65535" >&2
  exit 2
fi

mkdir -p "${ARTIFACT_DIR}"
COMPOSE=(docker compose -p "${PROJECT}" -f manifest/docker/docker-compose.yml -f manifest/docker/docker-compose.test.yml)

cleanup() {
  local code=$?
  trap - EXIT INT TERM
  set +e
  if [[ -n "${GOOSE_CONTAINER}" ]]; then
    docker rm -f "${GOOSE_CONTAINER}" >/dev/null 2>&1
  fi
  if ((KEEP_STACK == 0)); then
    "${COMPOSE[@]}" down -v --remove-orphans >"${ARTIFACT_DIR}/cleanup.log" 2>&1 || true
  fi
  exit "${code}"
}
trap cleanup EXIT INT TERM

SENTINELOPS_E2E_HTTP_PORT="${HTTP_PORT}" SENTINELOPS_E2E_MYSQL_PORT="${MYSQL_PORT}" \
  "${SCRIPT_DIR}/image-contract.sh" --output-dir "${ARTIFACT_DIR}/images"

SENTINELOPS_E2E_HTTP_PORT="${HTTP_PORT}" SENTINELOPS_E2E_MYSQL_PORT="${MYSQL_PORT}" \
  "${COMPOSE[@]}" up -d --wait >"${ARTIFACT_DIR}/compose-up.log" 2>&1
SENTINELOPS_E2E_HTTP_PORT="${HTTP_PORT}" SENTINELOPS_E2E_MYSQL_PORT="${MYSQL_PORT}" \
  "${COMPOSE[@]}" ps --format json >"${ARTIFACT_DIR}/compose-ps.json"

# 迁移容器必须成功完成，API/Worker/frontend 必须使用 P41 刚构建的镜像。
SENTINELOPS_E2E_HTTP_PORT="${HTTP_PORT}" SENTINELOPS_E2E_MYSQL_PORT="${MYSQL_PORT}" \
  "${COMPOSE[@]}" run --rm migrate >"${ARTIFACT_DIR}/migration-repeat.log" 2>&1

"${COMPOSE[@]}" exec -T mysql mysql -uroot -psentinelops-e2e-root-password \
  -e 'CREATE DATABASE IF NOT EXISTS sentinelops_p03 CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci' \
  >"${ARTIFACT_DIR}/create-test-database.log" 2>&1

export SENTINELOPS_TEST_DSN="root:sentinelops-e2e-root-password@tcp(127.0.0.1:${MYSQL_PORT})/sentinelops_p03?parseTime=true"
TOOLS_DIR="${ARTIFACT_DIR}/tools"
mkdir -p "${TOOLS_DIR}"
MIGRATE_IMAGE="$("${COMPOSE[@]}" images -q migrate | head -n 1)"
[[ -n "${MIGRATE_IMAGE}" ]] || { echo "FAIL: migrate image is unavailable" >&2; exit 1; }
GOOSE_CONTAINER="$(docker create "${MIGRATE_IMAGE}")"
docker cp "${GOOSE_CONTAINER}:/usr/local/bin/goose" "${TOOLS_DIR}/goose"
chmod 0755 "${TOOLS_DIR}/goose"
docker rm -f "${GOOSE_CONTAINER}" >/dev/null
GOOSE_CONTAINER=""
export SENTINELOPS_GOOSE_BIN="${TOOLS_DIR}/goose"
"${SENTINELOPS_GOOSE_BIN}" -version | rg -q 'v3\.27\.3' || {
  echo "FAIL: migrate image did not provide goose v3.27.3" >&2
  exit 1
}

go test ./internal/dao/mysql -run 'TestMigrationsUpFrom(EmptyDatabase|CurrentSchemaSnapshot)$' -count=1 \
  >"${ARTIFACT_DIR}/migration-tests.log" 2>&1

go test ./internal/ai/runtime ./internal/ai/effects ./internal/ai/workflow \
  -run '^TestFaultInjectionRepresentative$' -count=1 \
  >"${ARTIFACT_DIR}/fault-representative.log" 2>&1

go test ./internal/ai/runtime ./internal/ai/workflow ./internal/dao/mysql \
  -run 'Test(APIWorkerFocusedIntegration|LegacyRuntimeContractNeverBecomesDurableEligible|RevisionZeroConcurrentBootstrapUsesMySQLOnly|ClaimTwoWorkersOnlyOneOwnerGeneration|GenerationIncreasesAfterLeaseExpiry|StaleGenerationCannotWriteTruth|CreateRunSameSessionConcurrentOnlyOneSucceeds|SessionRunParkedKeepsExclusiveLock|TwoPhaseApproval|CheckpointFingerprint|ApprovalDecisionCAS|ExternalEffectSucceededIsReusedAfterToolReturnCrash|UnknownWindowParksRunAndKeepsSessionLockAtomically|UnknownEffectExpiredRunningExternalBecomesParked|SSE|Evidence)' \
  -count=1 >"${ARTIFACT_DIR}/integration-tests.log" 2>&1

npm ci --prefix web >"${ARTIFACT_DIR}/npm-ci.log" 2>&1
if ((INSTALL_BROWSER)); then
  (
    cd web
    npx playwright install --with-deps chromium
  ) >"${ARTIFACT_DIR}/playwright-install.log" 2>&1
fi

SENTINELOPS_E2E_BASE_URL="http://127.0.0.1:${HTTP_PORT}" \
SENTINELOPS_E2E_ADMIN_PASSWORD="sentinelops-e2e-admin-password" \
SENTINELOPS_E2E_JWT_SECRET="sentinelops-e2e-jwt-secret" \
  bash -c 'cd web && npx playwright test tests/e2e/approval-flow.spec.ts tests/e2e/unknown-effect.spec.ts --project=chromium --workers=1 --reporter=line' \
    >"${ARTIFACT_DIR}/playwright.log" 2>&1

echo "PASS: P41 integration and representative fault gates; artifacts=${ARTIFACT_DIR}"
