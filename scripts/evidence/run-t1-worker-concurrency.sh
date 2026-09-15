#!/usr/bin/env bash
# T1 复现脚本：K 个真实 Worker 进程 × M 个真实 Run 并发认领正确性 + 生产 Claim SQL EXPLAIN。
#
# 依赖：本地 dev MySQL（manifest/docker/docker-compose.dev.yml 的 3307）、goose v3.27.3。
# 用法：
#   SENTINELOPS_TEST_DSN='root:<password>@tcp(127.0.0.1:3307)/sentinelops_phase03?parseTime=true&loc=UTC&interpolateParams=true' \
#     scripts/evidence/run-t1-worker-concurrency.sh
#
# 说明：
#   * 只使用一次性数据库 sentinelops_phase03_killex_concw_*（测试结束自动删除）；
#   * DSN 建议带 interpolateParams=true，便于用 MySQL general_log 抓取服务端实际收到的
#     Claim SQL 原文（该选项只改变驱动侧参数插值方式，不改变 SQL 语义）；
#   * 设置 SENTINELOPS_CONCW_KEEP_DB=1 可保留实验库供人工核对（复用 killex 开关）。
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repository_root"

if [[ -z "${SENTINELOPS_TEST_DSN:-}" ]]; then
  echo "SENTINELOPS_TEST_DSN is required and must target the disposable sentinelops_phase03 database." >&2
  exit 2
fi

if ! command -v goose >/dev/null 2>&1 && [[ -z "${SENTINELOPS_GOOSE_BIN:-}" ]]; then
  echo "goose v3.27.3 is required (SENTINELOPS_GOOSE_BIN can point at a pinned binary)." >&2
  exit 2
fi

export SENTINELOPS_GOOSE_BIN="${SENTINELOPS_GOOSE_BIN:-$(command -v goose)}"
evidence_dir="${SENTINELOPS_CONCW_EVIDENCE_DIR:-output/interview-evidence/t1-worker-concurrency}"
mkdir -p "$evidence_dir"
# go test 的工作目录是包目录，证据路径必须绝对化。
export SENTINELOPS_CONCW_EVIDENCE_DIR="$(cd "$evidence_dir" && pwd)"

echo "== T1 K x M concurrent claim matrix =="
go test ./internal/ai/runtime/ -run '^TestWorkerConcurrentClaimMatrix$' -count=1 -v -timeout 30m

echo "== T1 small-scale claim SQL EXPLAIN (existing dev database, read-only) =="
go test ./internal/ai/runtime/ -run '^TestWorkerConcurrencyClaimExplainDevSmall$' -count=1 -v -timeout 10m

echo "== T1 evidence summary =="
python3 - "$SENTINELOPS_CONCW_EVIDENCE_DIR" <<'PY'
import json
import pathlib
import sys

directory = pathlib.Path(sys.argv[1])
summary_path = directory / "summary.json"
if summary_path.exists():
    for case in json.loads(summary_path.read_text()):
        print(f"{case['case']}: workers={case['workers']} runs={case['runs']} "
              f"duration_ms={case['duration_ms']} executions={case['total_executions']} "
              f"duplicate_claims={case['duplicate_valid_claims']} duplicate_executions={case['duplicate_executions']} "
              f"all_assertions_passed={case['all_assertions_passed']}")
for name in ("explain-small.json", "explain-case-a-4x100.json", "explain-representative.json"):
    path = directory / name
    if not path.exists():
        continue
    plan = json.loads(path.read_text())
    keys = ",".join(f"{table['table_name']}:{table['access_type']}/{table['key'] or '-'}" for table in plan["tables"])
    print(f"{name}: rows={plan['workflow_runs_row_count']} durable={plan['durable_v1_row_count']} "
          f"filesort={plan['using_filesort']} plan=[{keys}]")
PY

echo "T1 evidence directory: $SENTINELOPS_CONCW_EVIDENCE_DIR"
