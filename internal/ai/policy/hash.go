package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	appconfig "SentinelOps/internal/config"
)

const (
	proposalDomain = "fo/proposal/v1\x00"
	approvalDomain = "fo/approval/v1\x00"
	effectDomain   = "fo/effect/v1\x00"
)

// ErrPlaintextSecret 表示 Proposal 参数包含解析后的凭证而不是 Secret 引用。
var ErrPlaintextSecret = errors.New("plaintext secret is forbidden")

// Proposal 是进入 Policy identity 计算前的动作提案值。
type Proposal struct {
	ToolName                 string    `json:"tool_name"`
	ToolRevision             string    `json:"tool_revision"`
	ToolSchemaHash           string    `json:"tool_schema_hash"`
	RiskLevel                RiskLevel `json:"risk_level"`
	ArgumentsWithSecretRefs  any       `json:"arguments_with_secret_refs"`
	TargetScope              any       `json:"target_scope"`
	PolicyHash               string    `json:"policy_hash"`
	RuntimeCompatibilityHash string    `json:"runtime_compatibility_hash"`
}

// FrozenProposal 保存与调用方可变输入隔离的 canonical bytes 和稳定 Hash。
type FrozenProposal struct {
	canonicalJSON []byte
	hash          string
}

// FreezeProposal 校验并冻结 Proposal，后续调用方修改原 map 不影响身份。
func FreezeProposal(proposal Proposal) (FrozenProposal, error) {
	canonical, err := proposalCanonicalJSON(proposal)
	if err != nil {
		return FrozenProposal{}, err
	}
	return FrozenProposal{
		canonicalJSON: append([]byte(nil), canonical...),
		hash:          hashBytes(proposalDomain, canonical),
	}, nil
}

// CanonicalJSON 返回不可修改内部状态的 canonical bytes 副本。
func (p FrozenProposal) CanonicalJSON() []byte {
	return append([]byte(nil), p.canonicalJSON...)
}

// Hash 返回冻结 Proposal 的 `fo/proposal/v1` identity。
func (p FrozenProposal) Hash() string {
	return p.hash
}

// ProposalHash 计算 Secret 注入前 Proposal 的稳定 identity。
func ProposalHash(proposal Proposal) (string, error) {
	frozen, err := FreezeProposal(proposal)
	if err != nil {
		return "", err
	}
	return frozen.Hash(), nil
}

// ApprovalID 计算与 tool_call_id 无关的稳定 Approval identity。
func ApprovalID(runID, proposalHash string) (string, error) {
	if err := validateIdentityPart("run_id", runID); err != nil {
		return "", err
	}
	if err := validateSHA256("proposal_hash", proposalHash); err != nil {
		return "", err
	}
	return hashBytes(approvalDomain, []byte(runID), []byte{0}, []byte(proposalHash)), nil
}

// EffectKey 计算同一 Proposal 内按稳定 effect_step 隔离的 Effect identity。
func EffectKey(runID, proposalHash, effectStep string) (string, error) {
	if err := validateIdentityPart("run_id", runID); err != nil {
		return "", err
	}
	if err := validateSHA256("proposal_hash", proposalHash); err != nil {
		return "", err
	}
	if err := validateIdentityPart("effect_step", effectStep); err != nil {
		return "", err
	}
	return hashBytes(effectDomain, []byte(runID), []byte{0}, []byte(proposalHash), []byte{0}, []byte(effectStep)), nil
}

func proposalCanonicalJSON(proposal Proposal) ([]byte, error) {
	for name, value := range map[string]string{
		"tool_name":                  proposal.ToolName,
		"tool_revision":              proposal.ToolRevision,
		"tool_schema_hash":           proposal.ToolSchemaHash,
		"policy_hash":                proposal.PolicyHash,
		"runtime_compatibility_hash": proposal.RuntimeCompatibilityHash,
	} {
		if err := validateIdentityPart(name, value); err != nil {
			return nil, err
		}
	}
	if err := proposal.RiskLevel.Validate(); err != nil {
		return nil, err
	}
	if err := validateNoPlaintextSecrets(proposal.ArgumentsWithSecretRefs, "arguments_with_secret_refs"); err != nil {
		return nil, err
	}
	return CanonicalJSON(proposal)
}

func validateNoPlaintextSecrets(value any, path string) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	decoded, err := decodeJSON(raw)
	if err != nil {
		return err
	}
	return inspectSecrets(decoded, path, "")
}

func inspectSecrets(value any, path, key string) error {
	switch typed := value.(type) {
	case map[string]any:
		for childKey, child := range typed {
			if err := inspectSecrets(child, path+"."+childKey, childKey); err != nil {
				return err
			}
		}
	case []any:
		for index, child := range typed {
			if err := inspectSecrets(child, fmt.Sprintf("%s[%d]", path, index), ""); err != nil {
				return err
			}
		}
	case string:
		if typed == "" {
			return nil
		}
		if appconfig.SecretRef(typed).Validate() == nil {
			return nil
		}
		if isSensitiveKey(key) || containsSecretMaterial(typed) {
			return fmt.Errorf("%s contains resolved credential: %w", path, ErrPlaintextSecret)
		}
	default:
		if isSensitiveKey(key) && value != nil {
			return fmt.Errorf("%s must contain a Secret reference: %w", path, ErrPlaintextSecret)
		}
	}
	return nil
}

func validateIdentityPart(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", name)
	}
	if strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("%s contains NUL separator", name)
	}
	return nil
}

func validateSHA256(name, value string) error {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return fmt.Errorf("%s must be a lowercase SHA-256 hex digest", name)
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("%s must be a lowercase SHA-256 hex digest", name)
	}
	return nil
}

func hashBytes(domain string, parts ...[]byte) string {
	digest := sha256.New()
	_, _ = digest.Write([]byte(domain))
	for _, part := range parts {
		_, _ = digest.Write(part)
	}
	return hex.EncodeToString(digest.Sum(nil))
}
