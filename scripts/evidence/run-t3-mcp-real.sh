#!/usr/bin/env bash
# T3 复现脚本：真实 MCP 链路（Context7）+ Runtime 策略拒绝 + durable budget 落库。
#
# 依赖：
#   * Context7 容器在线（127.0.0.1:3333，匿名 /mcp）；
#   * 第二部分（durable budget）需要本地 dev MySQL 与 goose v3.27.3、SENTINELOPS_TEST_DSN。
#
# 用法：
#   SENTINELOPS_TEST_DSN='root:<password>@tcp(127.0.0.1:3307)/sentinelops_phase03?parseTime=true&loc=UTC&interpolateParams=true' \
#     scripts/evidence/run-t3-mcp-real.sh
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repository_root"

evidence_dir="${SENTINELOPS_T3_EVIDENCE_DIR:-output/interview-evidence/t3-mcp-real}"
mkdir -p "$evidence_dir"
export SENTINELOPS_T3_EVIDENCE_DIR="$(cd "$evidence_dir" && pwd)"

echo "== T3 real Context7 MCP chain (connect/initialize/tools-list/tools-call + policy rejections) =="
go test ./internal/ai/agent/mcp_pipeline/ -run '^TestMCPRealContext7Chain$' -count=1 -v -timeout 10m

if [[ -z "${SENTINELOPS_TEST_DSN:-}" ]]; then
  echo "SENTINELOPS_TEST_DSN is required for the durable budget part (real MCP call through MCPBudgetHook)." >&2
  exit 2
fi
export SENTINELOPS_GOOSE_BIN="${SENTINELOPS_GOOSE_BIN:-$(command -v goose)}"

echo "== T3 real MCP call through durable budget (MySQL truth) =="
go test ./internal/ai/runtime/ -run '^TestMCPRealCallThroughDurableBudget$' -count=1 -v -timeout 10m

echo "== T3 evidence summary =="
python3 - "$SENTINELOPS_T3_EVIDENCE_DIR" <<'PY'
import json
import pathlib
import sys

directory = pathlib.Path(sys.argv[1])
tools = json.loads((directory / "tools-list.json").read_text())
call = json.loads((directory / "tool-call.json").read_text())
rejection = json.loads((directory / "policy-rejection.json").read_text())
budget_path = directory / "budget-durable-call.json"
print(f"server={tools['server']['name']} endpoint={tools['server']['endpoint']} transport={tools['server']['transport']}")
print(f"server raw tools={[t['raw_name'] for t in tools['server_raw_tools']]}")
print(f"runtime exposed tools={[t['raw_name'] for t in tools['runtime_exposed_tools']]} catalog_hash={tools['runtime_catalog_hash']}")
print(f"tools/call success={call['call']['success']} duration_ms={call['call']['duration_ms']} bytes={call['call']['result_bytes']}")
print(f"allowlist rejection: exposed={rejection['allowlist_case']['runtime_exposed_tool_names']} "
      f"tools_call_requests={rejection['allowlist_case']['tools_call_requests']}")
print(f"write-tool rejection: {rejection['write_tool_case']['runtime_rejection']} "
      f"tools_call_requests={rejection['write_tool_case']['tools_call_requests']}")
if budget_path.exists():
    budget = json.loads(budget_path.read_text())
    print(f"durable budget: events={[e['event_type'] for e in budget['budget_events']]} usage={budget['budget_usage_json']}")
PY

echo "T3 evidence directory: $SENTINELOPS_T3_EVIDENCE_DIR"
