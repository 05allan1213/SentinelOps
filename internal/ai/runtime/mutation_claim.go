package runtime

import (
	"context"
	"strings"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
)

// 未调用却宣布成功：模型可能在没有任何 Effect/Approval 事实的情况下宣称
// “已保存 / 已执行 / 已通知”。Runtime 以事实为准，这类答案会带上确定性提示，
// 避免用户把未经服务端证实的副作用当成既成事实。
const mutationClaimNotice = "【未验证】本轮没有记录到对应的 Effect 或审批事实，以下“已执行/已保存”类描述无法被服务端证实。\n\n"

// mutationClaimPhrases 是高置信度的完成态声明。仅在 Run 没有任何变更事实时
// 触发提示，且被否定词修饰的声明（如“未保存”）不会命中。
var mutationClaimPhrases = []string{
	"已保存", "已写入", "已入库", "已存储", "已生成并保存", "已成功保存",
	"已成功调用", "已创建并保存", "已执行", "已成功执行",
	"已发送", "已推送", "已通知", "已封禁", "已阻断", "已隔离",
	"successfully called", "successfully executed", "has been saved", "was saved",
}

// mutationClaimNegations 用于排除“未保存 / 没有调用 / 无法执行”等否定声明。
var mutationClaimNegations = []string{"未", "没有", "没", "无法", "不能", "尚未", "not ", "did not", "no "}

// MutationToolNames 从冻结 Catalog 派生全部 Mutation 工具名（L1/L2 且带 Effect）。
func MutationToolNames() []string {
	entries := policy.CatalogEntries()
	names := make([]string, 0, 8)
	for name, entry := range entries {
		if entry.Risk == policy.RiskL0 || entry.EffectType == "" {
			continue
		}
		names = append(names, name)
	}
	return names
}

// RunMutationFacts 判断 Run 是否已经记录了任何 Effect 或 Approval 事实。
// 查询不可用时返回 true（视为已有事实），避免在事实未知时误报。
func RunMutationFacts(ctx context.Context, store *workflow.GORMStore, runID string) bool {
	if store == nil || strings.TrimSpace(runID) == "" {
		return true
	}
	db := store.DB()
	if db == nil {
		return true
	}
	var effects, approvals int64
	if err := db.WithContext(ctx).Model(&mysql.AgentEffect{}).Where("run_id = ?", runID).Count(&effects).Error; err != nil {
		return true
	}
	if err := db.WithContext(ctx).Model(&mysql.AgentApproval{}).Where("run_id = ?", runID).Count(&approvals).Error; err != nil {
		return true
	}
	return effects > 0 || approvals > 0
}

// HasUnverifiedMutationClaim 判断答案是否在缺少变更事实时宣称已执行副作用。
func HasUnverifiedMutationClaim(answer string, mutationFacts bool) bool {
	if mutationFacts {
		return false
	}
	if strings.TrimSpace(answer) == "" {
		return false
	}
	lowered := strings.ToLower(answer)
	for _, name := range MutationToolNames() {
		if containsPositiveClaim(lowered, strings.ToLower(name)) {
			return true
		}
	}
	for _, phrase := range mutationClaimPhrases {
		if containsPositiveClaim(lowered, strings.ToLower(phrase)) {
			return true
		}
	}
	return false
}

// withMutationClaimNotice 在命中未证实声明时返回带提示的答案。
func withMutationClaimNotice(answer string, mutationFacts bool) (string, bool) {
	if !HasUnverifiedMutationClaim(answer, mutationFacts) {
		return answer, false
	}
	return mutationClaimNotice + answer, true
}

// containsPositiveClaim 忽略被否定词修饰的出现位置，避免把“未调用工具”当成声明。
func containsPositiveClaim(text, needle string) bool {
	if needle == "" {
		return false
	}
	index := 0
	for {
		pos := strings.Index(text[index:], needle)
		if pos < 0 {
			return false
		}
		absolute := index + pos
		if !claimIsNegated(text, absolute) {
			return true
		}
		index = absolute + len(needle)
	}
}

func claimIsNegated(text string, at int) bool {
	const window = 6
	runes := []rune(text[:at])
	start := len(runes) - window
	if start < 0 {
		start = 0
	}
	prefix := strings.ToLower(string(runes[start:]))
	for _, negation := range mutationClaimNegations {
		if strings.Contains(prefix, negation) {
			return true
		}
	}
	return false
}
