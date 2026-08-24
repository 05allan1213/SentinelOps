// ops_subagent.go 智能运维 SubAgent：将用户 query 直接传给 Ops Agent 流式执行。
package subagents

import (
	"context"
	"errors"
	"io"
	"strings"

	"SentinelOps/internal/ai/agent/base"
	"SentinelOps/internal/ai/agent/ops_pipeline"
	"SentinelOps/internal/ai/intent/core"
)

type OpsSubAgent struct{}

func (a *OpsSubAgent) Name() core.IntentType { return core.IntentOps }

func (a *OpsSubAgent) Execute(ctx context.Context, task *core.Task, callback core.StreamCallback) (*core.Result, error) {
	callback(core.IntentStatus, "[智能运维 Agent 处理中...]")

	runner, err := ops_pipeline.GetOpsAgent(ctx)
	if err != nil {
		return &core.Result{Intent: core.IntentOps, Error: err}, err
	}

	stream, err := runner.Stream(ctx, &base.UserMessage{Query: task.Query})
	if err != nil {
		return &core.Result{Intent: core.IntentOps, Error: err}, err
	}
	defer stream.Close()

	var sb strings.Builder
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return &core.Result{Intent: core.IntentOps, Error: err}, err
		}
		if msg != nil && msg.Content != "" {
			callback(core.IntentOps, msg.Content)
			sb.WriteString(msg.Content)
		}
	}

	return &core.Result{Intent: core.IntentOps, Content: sb.String()}, nil
}

func init() { core.RegisterSubAgent(&OpsSubAgent{}) }
