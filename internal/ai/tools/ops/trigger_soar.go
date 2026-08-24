// Package ops 提供 ChatOps 规划与叶子动作工具。
package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"SentinelOps/internal/ai/policy"
	dao "SentinelOps/internal/dao/mysql"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// TriggerOpsProposalInput 是模型提出但尚未获准执行的叶子 Mutation。
// ArgumentsJSON 只保存业务参数；风险、revision、schema 与 Effect step 均由服务端 Catalog 补齐。
type TriggerOpsProposalInput struct {
	ToolName      string `json:"tool_name" jsonschema:"description=Catalog 已登记的 L1/L2 叶子 Tool 名称,required=true"`
	ArgumentsJSON string `json:"arguments_json" jsonschema:"description=叶子 Tool 参数 JSON object,required=true"`
}

type TriggerOpsInput struct {
	EventID   string                    `json:"event_id" jsonschema:"description=待规划的安全事件 ID,required=true"`
	Proposals []TriggerOpsProposalInput `json:"proposals,omitempty" jsonschema:"description=待规范化的逐项 Mutation Proposal；为空时只返回事件事实"`
}

// TriggerOpsProposal 是只读规划结果，不是执行授权或 Effect。
type TriggerOpsProposal struct {
	ToolName       string            `json:"tool_name"`
	ToolRevision   string            `json:"tool_revision"`
	ToolSchemaHash string            `json:"tool_schema_hash"`
	Risk           policy.RiskLevel  `json:"risk_level"`
	EffectType     policy.EffectType `json:"effect_type"`
	EffectSteps    []string          `json:"effect_steps"`
	Arguments      json.RawMessage   `json:"arguments"`
}

// TriggerOpsOutput 是确定性的只读规划响应。
type TriggerOpsOutput struct {
	EventID       string               `json:"event_id"`
	EventTitle    string               `json:"event_title"`
	EventSeverity string               `json:"event_severity"`
	Proposals     []TriggerOpsProposal `json:"proposals"`
}

// NewTriggerOpsTool 创建只读 ChatOps Proposal 规划工具。
func NewTriggerOpsTool() tool.InvokableTool {
	t, err := utils.InferOptionableTool(
		"trigger_ops",
		"基于已查询的事件事实规范化逐项运维 Proposal。只规划、不执行动作；每个 L1/L2 叶子 Tool 后续独立审批。",
		func(ctx context.Context, input *TriggerOpsInput, opts ...tool.Option) (string, error) {
			if err := policy.Authorize(ctx, policy.PermissionViewScoped, policy.Resource{}); err != nil {
				return "", err
			}
			if strings.TrimSpace(input.EventID) == "" {
				return "", fmt.Errorf("event_id 不能为空")
			}
			event, err := dao.GetEventByID(ctx, input.EventID)
			if err != nil {
				return "", fmt.Errorf("获取事件失败: %w", err)
			}
			proposals, err := buildTriggerOpsProposals(input.Proposals)
			if err != nil {
				return "", err
			}
			out, err := json.Marshal(TriggerOpsOutput{
				EventID: input.EventID, EventTitle: event.Title, EventSeverity: event.Severity, Proposals: proposals,
			})
			if err != nil {
				return "", fmt.Errorf("编码运维 Proposal: %w", err)
			}
			return string(out), nil
		},
	)
	if err != nil {
		panic(err)
	}
	return t
}

func buildTriggerOpsProposals(inputs []TriggerOpsProposalInput) ([]TriggerOpsProposal, error) {
	proposals := make([]TriggerOpsProposal, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	for index, input := range inputs {
		entry, err := policy.LookupCatalog(strings.TrimSpace(input.ToolName))
		if err != nil {
			return nil, fmt.Errorf("proposal[%d]: %w", index, err)
		}
		if entry.Audience != policy.AudienceDurable || entry.Risk == policy.RiskL0 {
			return nil, fmt.Errorf("proposal[%d] tool %q is not a durable Mutation Tool", index, input.ToolName)
		}
		argumentsJSON := input.ArgumentsJSON
		if strings.TrimSpace(argumentsJSON) == "" {
			argumentsJSON = "{}"
		}
		canonical, err := policy.CanonicalToolArgumentsJSON([]byte(argumentsJSON))
		if err != nil {
			return nil, fmt.Errorf("proposal[%d] canonical arguments: %w", index, err)
		}
		identity := entry.Name + "\x00" + string(canonical)
		if _, duplicate := seen[identity]; duplicate {
			return nil, fmt.Errorf("proposal[%d] duplicates an earlier proposal", index)
		}
		seen[identity] = struct{}{}
		proposals = append(proposals, TriggerOpsProposal{
			ToolName: entry.Name, ToolRevision: entry.Revision, ToolSchemaHash: entry.SchemaHash,
			Risk: entry.Risk, EffectType: entry.EffectType, EffectSteps: entry.EffectSteps,
			Arguments: json.RawMessage(canonical),
		})
	}
	return proposals, nil
}
