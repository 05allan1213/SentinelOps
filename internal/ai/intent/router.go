// router.go 意图路由节点：调用 LLM（QwenV3 Quick）对用户查询进行意图识别与置信度评估。
//
// 本文件仅用于【标准模式（deep_thinking=false）】。深度思考模式下调用方直接调用 plan_pipeline，
// 不经过此路由节点。
//
// 流程：System Prompt（5 类意图示例）+ User query → LLM → JSON{"intent","confidence"} → RouterOutput
//   - 置信度 ≥ 0.70：路由到对应 SubAgent
//   - 置信度 < 0.70：降级为 IntentChat，由 Chat Agent 自然追问澄清
//
// 容错：模型调用失败、JSON 解析错误、未知意图值 → 统一降级为 IntentChat + 置信度 0.0。
package intent

import (
	"context"
	"encoding/json"
	"fmt"

	"SentinelOps/internal/ai/models"
	"SentinelOps/internal/ai/prompt/routing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
)

// confidenceThreshold 置信度阈值，低于此值降级为 IntentChat。
const confidenceThreshold = 0.70

// RouterOutput 路由节点输出，供 Executor 消费。
type RouterOutput struct {
	Input      *IntentInput // 原始输入，透传给 Executor
	IntentType IntentType   // 意图识别结果，置信度不足或失败时降级为 IntentChat
	Confidence float64      // LLM 置信度（0.0~1.0），解析失败时为 0.0
}

// newRouterLambda 创建路由 Lambda 节点：LLM 意图识别 → 置信度检查 → 推送路由状态。
// 失败时降级为 IntentChat，保证路由不中断。
func newRouterLambda(ctx context.Context) (*compose.Lambda, error) {
	chatModel, err := models.ChatDefault(ctx)
	if err != nil {
		return nil, fmt.Errorf("create router model: %w", err)
	}

	lambda := compose.InvokableLambda(func(ctx context.Context, input *IntentInput) (*RouterOutput, error) {
		// 调用 LLM 进行意图识别（含置信度评估）；失败时降级为 IntentChat
		intentType, confidence, err := recognizeIntent(ctx, chatModel, input.Query)
		if err != nil {
			g.Log().Warningf(ctx, "[Router] 意图识别失败，降级为 chat | err=%v", err)
			intentType = IntentChat
			confidence = 0.0
		}

		g.Log().Infof(ctx, "[Router] 意图识别完成 | intent=%s | confidence=%.2f | query=%q",
			intentType, confidence, input.Query)

		out := &RouterOutput{
			Input:      input,
			IntentType: intentType,
			Confidence: confidence,
		}

		// 置信度检查：低于阈值时降级为 IntentChat，交由 Chat Agent 自然澄清
		if confidence < confidenceThreshold && intentType != IntentChat {
			g.Log().Debugf(ctx, "[Router] 置信度不足，降级为 chat | intent=%s | confidence=%.2f",
				intentType, confidence)
			out.IntentType = IntentChat
		}

		// 通过 Callback 向调用方实时推送路由状态，供前端 agentStatus 显示
		if input.Callback != nil {
			input.Callback(IntentStatus, fmt.Sprintf("[路由到: %s | 置信度: %.0f%%]",
				intentType, confidence*100))
		}

		return out, nil
	})
	return lambda, nil
}

// recognizeIntent 使用 default Profile 识别意图并返回置信度。
func recognizeIntent(ctx context.Context, m model.ToolCallingChatModel, query string) (IntentType, float64, error) {
	messages := []*schema.Message{
		schema.SystemMessage(buildRecognitionSystemPrompt()),
		schema.UserMessage(query),
	}

	resp, err := m.Generate(ctx, messages)
	if err != nil {
		return IntentChat, 0.0, fmt.Errorf("recognize intent: %w", err)
	}

	intentType, confidence := parseIntentWithConfidence(ctx, resp.Content)
	return intentType, confidence, nil
}

// buildRecognitionSystemPrompt 构建意图识别 System Prompt。
// 包含 6 类意图定义、含置信度的 few-shot 示例和格式约束（只返回 JSON）。
// 注：plan 意图已移除——深度思考模式下由 chatsvc.ExecuteIntentDeepThink 直接调用 Plan Agent。
// 完整提示词内容参见 internal/ai/prompt/routing.go
func buildRecognitionSystemPrompt() string {
	return routing.Router
}

// parseIntentWithConfidence 解析 LLM 返回的 JSON，提取 intent 和 confidence。
// JSON 格式错误、intent 为空/未知、confidence 缺失时均回退为 IntentChat + 0.0。
func parseIntentWithConfidence(ctx context.Context, content string) (IntentType, float64) {
	var result struct {
		Intent     string  `json:"intent"`
		Confidence float64 `json:"confidence"`
	}

	if err := json.Unmarshal([]byte(content), &result); err != nil {
		g.Log().Debugf(ctx, "[Router] 意图类型解析失败，降级为 chat | raw=%q", content)
		return IntentChat, 0.0
	}

	// 将 LLM 返回的字符串映射为类型安全的 IntentType 常量
	var intentType IntentType
	switch result.Intent {
	case "event":
		intentType = IntentEvent
	case "report":
		intentType = IntentReport
	case "risk":
		intentType = IntentRisk
	case "solve":
		intentType = IntentSolve
	case "intel":
		intentType = IntentIntel
	case "ops":
		intentType = IntentOps
	default:
		// "chat" 或任何未知值均降级为 IntentChat（安全兜底）
		intentType = IntentChat
	}

	return intentType, result.Confidence
}
