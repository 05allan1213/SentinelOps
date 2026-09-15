#!/usr/bin/env bash
# T4 复现脚本：webhook_out 真实公网 HTTPS（httpbin 规定用例 + webhook.site 服务端回显 + 4xx/5xx 分类）。
#
# 依赖：本地 dev MySQL 3307、goose v3.27.3、可访问公网的 HTTPS 出口（本机经 127.0.0.1:7897 代理）。
# 用法：
#   SENTINELOPS_TEST_DSN='root:<password>@tcp(127.0.0.1:3307)/sentinelops_phase03?parseTime=true&loc=UTC&interpolateParams=true' \
#     scripts/evidence/run-t4-webhook-real.sh
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repository_root"

if [[ -z "${SENTINELOPS_TEST_DSN:-}" ]]; then
  echo "SENTINELOPS_TEST_DSN is required (disposable sentinelops_phase03 database)." >&2
  exit 2
fi
export SENTINELOPS_GOOSE_BIN="${SENTINELOPS_GOOSE_BIN:-$(command -v goose)}"

evidence_dir="${SENTINELOPS_T4_EVIDENCE_DIR:-output/interview-evidence/t4-webhook-real}"
mkdir -p "$evidence_dir"
export SENTINELOPS_T4_EVIDENCE_DIR="$(cd "$evidence_dir" && pwd)"

echo "== T4 real public HTTPS webhook_out (production effect path) =="
go test ./internal/ai/runtime/ -run '^TestWebhookOutRealPublicHTTPS$' -count=1 -v -timeout 15m

echo "== T4 evidence summary =="
python3 - "$SENTINELOPS_T4_EVIDENCE_DIR" <<'PY'
import json
import pathlib
import sys

directory = pathlib.Path(sys.argv[1])
success = json.loads((directory / "success.json").read_text())
errors = json.loads((directory / "error-cases.json").read_text())
httpbin = success["httpbin"]
site = success["webhook_site"]
print(f"httpbin {httpbin['target_url']}: effect_status={httpbin['effect']['status']} "
      f"external_reference=effect_key:{httpbin['effect']['external_reference'] == httpbin['effect']['idempotency_key']}")
echo = site.get("server_echo", {})
print(f"webhook.site: idempotency_key_matches_effect_key={echo.get('idempotency_key_matches_effect_key')} "
      f"content_type={echo.get('content_type')} body_matches={echo.get('body_matches_payload')} "
      f"x_request_id_sent={echo.get('x_request_id_sent')} external_reference={site['effect']['external_reference']}")
for name in ("http_404", "http_503", "x_request_id_case"):
    case = errors[name]
    effect = case.get("effect") or {}
    print(f"{name}: effect_status={effect.get('status')} external_reference={effect.get('external_reference')} "
          f"error={effect.get('last_error')}")
PY

echo "T4 evidence directory: $SENTINELOPS_T4_EVIDENCE_DIR"
