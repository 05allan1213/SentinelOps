package evidence

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	// ErrInvalidCitation 表示引用不属于当前 Run 或其完整性字段不匹配。
	ErrInvalidCitation = errors.New("invalid citation")
	// ErrExpiredEvidence 表示引用指向的 Evidence 已超过允许的有效窗口。
	ErrExpiredEvidence = errors.New("evidence is expired")
)

// DefaultEvidenceMaxAge 是 Evidence 没有显式刷新时间时使用的保守有效窗口。
const DefaultEvidenceMaxAge = 24 * time.Hour

// Citation 是模型输出的 Evidence 引用；冗余字段用于防止只凭 ID 的篡改。
type Citation struct {
	RunID          string `json:"run_id"`
	EvidenceID     string `json:"evidence_id"`
	SourceVersion  string `json:"source_version"`
	ContentHash    string `json:"content_hash"`
	AccessScope    string `json:"access_scope"`
	IndexedVersion uint64 `json:"indexed_version,omitempty"`
}

// RunEvidence 是一次 Run 的有效 Evidence 快照；Refs 不跨 Run 复用。
type RunEvidence struct {
	RunID string
	Scope Scope
	Refs  map[string]EvidenceRef
}

// Grounding 表示最终答案是否有当前 Run 的可核对引用。
type Grounding string

const (
	GroundingGrounded     Grounding = "grounded"
	GroundingInference    Grounding = "inference"
	GroundingInsufficient Grounding = "insufficient_information"
)

// AnswerValidation 是最终输出的确定性验证结果。
type AnswerValidation struct {
	Grounding Grounding
	Reason    string
	Citations []Citation
}

var citationMarkerPattern = regexp.MustCompile(`(?i)\[(?:evidence:|\^)(evidence-v1:[0-9a-f]{64})\]`)
var citationAnyPattern = regexp.MustCompile(`(?i)\[(?:evidence:|\^)([^\]]+)\]`)

// CitationMarker 返回唯一、可被 ValidateAnswer 解析的引用格式。
func CitationMarker(evidenceID string) string {
	return "[evidence:" + evidenceID + "]"
}

// ValidateCitation 验证引用只来自当前 Run，并与完整 Evidence 快照逐字段一致。
func ValidateCitation(c Citation, current RunEvidence, now time.Time) error {
	if strings.TrimSpace(c.RunID) == "" || c.RunID != current.RunID || strings.TrimSpace(c.EvidenceID) == "" {
		return fmt.Errorf("%w: run or evidence identity mismatch", ErrInvalidCitation)
	}
	ref, ok := current.Refs[c.EvidenceID]
	if !ok {
		return fmt.Errorf("%w: evidence was not retrieved by this Run", ErrInvalidCitation)
	}
	if err := ref.Validate(); err != nil {
		return fmt.Errorf("%w: stored evidence: %v", ErrInvalidCitation, err)
	}
	if c.SourceVersion != ref.SourceVersion || c.ContentHash != ref.ContentHash || c.AccessScope != ref.AccessScope || (c.IndexedVersion != 0 && c.IndexedVersion != ref.IndexedVersion) {
		return fmt.Errorf("%w: version, hash, scope or index version mismatch", ErrInvalidCitation)
	}
	if !current.Scope.Allows(ref.AccessScope) {
		return fmt.Errorf("%w: evidence is outside current Scope", ErrInvalidCitation)
	}
	if !now.IsZero() && now.Sub(ref.RetrievedAt) > DefaultEvidenceMaxAge {
		return fmt.Errorf("%w: retrieved_at=%s", ErrExpiredEvidence, ref.RetrievedAt.UTC().Format(time.RFC3339))
	}
	return nil
}

// ExtractCitations 提取答案中的 canonical Evidence markers。
func ExtractCitations(answer string) []Citation {
	matches := citationMarkerPattern.FindAllStringSubmatch(answer, -1)
	result := make([]Citation, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		if len(match) != 2 || match[1] == "" {
			continue
		}
		if _, ok := seen[match[1]]; ok {
			continue
		}
		seen[match[1]] = struct{}{}
		result = append(result, Citation{EvidenceID: match[1]})
	}
	return result
}

// ValidateAnswer 验证最终答案中的所有引用；无引用不伪装为 grounded。
func ValidateAnswer(answer string, current RunEvidence, now time.Time) (AnswerValidation, error) {
	citations := ExtractCitations(answer)
	if len(citationAnyPattern.FindAllStringSubmatch(answer, -1)) != len(citations) {
		return AnswerValidation{}, fmt.Errorf("%w: malformed Evidence marker", ErrInvalidCitation)
	}
	if len(citations) == 0 {
		return AnswerValidation{Grounding: GroundingInference, Reason: "无当前 Run Evidence 引用，结论标为推断/信息不足"}, nil
	}
	for i := range citations {
		citations[i].RunID = current.RunID
		ref, ok := current.Refs[citations[i].EvidenceID]
		if !ok {
			return AnswerValidation{}, fmt.Errorf("%w: %s", ErrInvalidCitation, citations[i].EvidenceID)
		}
		citations[i].SourceVersion = ref.SourceVersion
		citations[i].ContentHash = ref.ContentHash
		citations[i].AccessScope = ref.AccessScope
		citations[i].IndexedVersion = ref.IndexedVersion
		if err := ValidateCitation(citations[i], current, now); err != nil {
			return AnswerValidation{}, err
		}
	}
	return AnswerValidation{Grounding: GroundingGrounded, Reason: "引用来自当前 Run 的有效 Evidence", Citations: citations}, nil
}

// FinalizeAnswer 为无证据答案添加确定性标记；有证据答案保持模型文本不变。
func FinalizeAnswer(answer string, current RunEvidence, now time.Time) (string, AnswerValidation, error) {
	result, err := ValidateAnswer(answer, current, now)
	if err != nil {
		return "", AnswerValidation{}, err
	}
	if result.Grounding == GroundingInference {
		return "推断/信息不足：" + strings.TrimSpace(answer), result, nil
	}
	return answer, result, nil
}
