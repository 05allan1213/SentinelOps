#!/usr/bin/env bash
# T2 复现脚本：Hybrid RAG 小规模真实评测（Dense / BM25-only / Hybrid / Hybrid+Rewrite + ACL）。
#
# 依赖：
#   * SENTINELOPS_MODEL_API_KEY（Embedding + Chat Provider；缺失时测试以 SKIPPED_BLOCKED_BY_SECRET 跳过）；
#   * 本地 Milvus（19530）与 SentinelOps Redis（16379）已启动：docker compose -f manifest/docker/docker-compose.dev.yml up -d etcd minio standalone redis
#   * 本地 dev MySQL 3307、goose v3.27.3、SENTINELOPS_TEST_DSN（一次性评测库）。
#
# 用法：
#   set -a; . /home/monody/project/.env; set +a
#   SENTINELOPS_TEST_DSN='root:<password>@tcp(127.0.0.1:3307)/sentinelops_phase03?parseTime=true&loc=UTC&interpolateParams=true' \
#     scripts/evidence/run-t2-rag-eval.sh
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repository_root"

if [[ -z "${SENTINELOPS_TEST_DSN:-}" ]]; then
  echo "SENTINELOPS_TEST_DSN is required (disposable sentinelops_phase03 database)." >&2
  exit 2
fi
if [[ -z "${SENTINELOPS_MODEL_API_KEY:-}" ]]; then
  echo "SENTINELOPS_MODEL_API_KEY: MISSING -> T2 will be SKIPPED_BLOCKED_BY_SECRET" >&2
fi
export SENTINELOPS_GOOSE_BIN="${SENTINELOPS_GOOSE_BIN:-$(command -v goose)}"

evidence_dir="${SENTINELOPS_T2_EVIDENCE_DIR:-output/interview-evidence/t2-rag-eval}"
mkdir -p "$evidence_dir"
export SENTINELOPS_T2_EVIDENCE_DIR="$(cd "$evidence_dir" && pwd)"
mkdir -p "$SENTINELOPS_T2_EVIDENCE_DIR/observations"

echo "== T2 hybrid RAG evaluation (dense / bm25 ablation / hybrid / hybrid+rewrite + ACL) =="
go test ./internal/ai/retrieval/ -run '^TestHybridRAGEvaluationEvidence$' -count=1 -v -timeout 45m \
  | tee "$SENTINELOPS_T2_EVIDENCE_DIR/observations/run-full-go-test.log"

echo "== T2 evidence summary =="
python3 - "$SENTINELOPS_T2_EVIDENCE_DIR" <<'PY'
import json
import pathlib
import sys

directory = pathlib.Path(sys.argv[1])
metrics = json.loads((directory / "metrics.json").read_text())
for row in metrics["metrics"]:
    if row["scope"] == "all":
        print(f"{row['mode']:16s} n={row['queries']:2d} hit@1={row['hit_at_1']:.3f} "
              f"hit@3={row['hit_at_3']:.3f} hit@5={row['hit_at_5']:.3f} mrr={row['mrr_at_5']:.3f}")
acl = json.loads((directory / "acl-results.json").read_text())
for fact in acl:
    print(f"acl/{fact['mode']}: raw_contains_unauthorized={fact['raw_candidates_contain_unauthorized']} "
          f"final_contains_unauthorized={fact['final_evidence_contains_unauthorized']} "
          f"final_contains_authorized={fact['final_evidence_contains_authorized']}")
PY

echo "T2 evidence directory: $SENTINELOPS_T2_EVIDENCE_DIR"
