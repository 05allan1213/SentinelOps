package ops_pipeline

import "SentinelOps/internal/ai/ops/ctxkey"

// runIDCtxKey 用于在 context 中传递当前运维任务 ID，供工具层写步骤记录。
type runIDCtxKey = ctxkey.RunID
