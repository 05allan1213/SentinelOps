#!/usr/bin/env bash
# S1 故障注入复现脚本：真实 Worker 崩溃 → 租约过期 → 第二 Worker 接管 → fencing 拒绝旧 generation。
#
# 依赖：本地 dev MySQL（manifest/docker/docker-compose.dev.yml 的 3307）、goose v3.27.3。
# 用法：
#   SENTINELOPS_TEST_DSN='root:<password>@tcp(127.0.0.1:3307)/sentinelops_phase03?parseTime=true&loc=UTC' \
#     scripts/faultinjection/worker-kill-fencing.sh
#
# 脚本只使用一次性数据库 sentinelops_phase03_killex_*（测试结束自动删除）；
# 设置 SENTINELOPS_KILLEX_KEEP_DB=1 可以保留实验库供人工核对。
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repository_root"

if [[ -z "${SENTINELOPS_TEST_DSN:-}" ]]; then
  echo "SENTINELOPS_TEST_DSN is required and must target the disposable sentinelops_phase03 database." >&2
  echo "Example (local dev compose defaults):" >&2
  echo "  export SENTINELOPS_TEST_DSN='root:<compose-mysql-password>@tcp(127.0.0.1:3307)/sentinelops_phase03?parseTime=true&loc=UTC'" >&2
  exit 2
fi

if ! command -v goose >/dev/null 2>&1; then
  echo "goose v3.27.3 is required (SENTINELOPS_GOOSE_BIN can point at a pinned binary)." >&2
  exit 2
fi

evidence_dir="${SENTINELOPS_KILLEX_EVIDENCE_DIR:-output/fault-injection}"
mkdir -p "$evidence_dir"
# go test 的工作目录是包目录，证据路径必须绝对化，否则会写进 internal/ai/runtime/output。
evidence_dir="$(cd "$evidence_dir" && pwd)"

run_case() {
  local name="$1"
  local test_name="$2"
  local evidence_file="$evidence_dir/${name}.json"
  echo "== ${name} =="
  SENTINELOPS_KILLEX_EVIDENCE="$evidence_file" \
    go test ./internal/ai/runtime/ -run "^${test_name}$" -count=1 -v
  echo
}

run_case "worker-kill-claim-only-takeover" "TestWorkerProcessKillLeaseTakeoverFencing"
run_case "worker-kill-reap-closes-stale-attempt" "TestWorkerProcessKillReapClosesStaleAttempt"
run_case "fencing-boundary-is-generation" "TestWorkerProcessKillFencingBoundaryIsGeneration"

echo "== evidence summary =="
python3 - "$evidence_dir" <<'PY'
import json
import pathlib
import sys

directory = pathlib.Path(sys.argv[1])
for path in sorted(directory.glob("*.json")):
    payload = json.loads(path.read_text())
    print(f"{path.name}:")
    print(f"  run={payload.get('run_id')} lease={payload.get('lease_duration_ms')}ms")
    if payload.get("claim_a_generation"):
        print(f"  A: generation={payload.get('claim_a_generation')} attempt={payload.get('claim_a_attempt')} lease_until={payload.get('claim_a_lease_until')}")
    if payload.get("takeover_owner"):
        print(f"  B: owner={payload.get('takeover_owner')} generation={payload.get('takeover_generation')} attempt={payload.get('takeover_attempt')} after_expiry={payload.get('takeover_after_lease_expiry')}")
    if payload.get("stale_write_during_takeover"):
        stale = payload["stale_write_during_takeover"]
        print(f"  stale during takeover: heartbeat={stale['heartbeat']} transition={stale['transition']} complete={stale['complete']}")
    if payload.get("boundary_heartbeat_before_expiry"):
        print(f"  boundary: before_expiry={payload['boundary_heartbeat_before_expiry']} after_reap={payload.get('boundary_heartbeat_after_reap')}")
    if payload.get("final_run_status"):
        print(f"  final: status={payload['final_run_status']} attempt={payload['final_attempt']} generation={payload['final_lease_generation']} last_event_seq={payload['final_last_event_seq']}")
    for attempt in payload.get("attempts", []):
        print(f"  attempt {attempt['attempt']}: worker={attempt['worker']} status={attempt['status']} failure={attempt['failure_code'] or '-'} open={attempt['open_projection']}")
PY
