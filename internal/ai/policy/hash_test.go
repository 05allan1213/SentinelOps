package policy

import (
	"errors"
	"strings"
	"testing"
)

func TestProposalIdentityGoldenVectors(t *testing.T) {
	proposal := Proposal{
		ToolName:                 "block_ip",
		ToolRevision:             "v1",
		ToolSchemaHash:           "schema-v1",
		RiskLevel:                RiskL2,
		ArgumentsWithSecretRefs:  map[string]any{"ip": "192.0.2.1", "ttl": 3600, "credential_ref": "env:FIREWALL_TOKEN"},
		TargetScope:              map[string]any{"asset_id": "edge-1"},
		PolicyHash:               "policy-v1",
		RuntimeCompatibilityHash: "runtime-v1",
	}

	proposalHash, err := ProposalHash(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if want := "82d1c4aa1f361a1b2b16ec5d31b38aea8188b860757a2830cf4c0c4dd18775d5"; proposalHash != want {
		t.Fatalf("ProposalHash() = %s, want %s", proposalHash, want)
	}
	approvalID, err := ApprovalID("run-123", proposalHash)
	if err != nil {
		t.Fatal(err)
	}
	if want := "bd3373f37848e4a6e9493e9ec895012f94ee4e07d2a3c5942be08a0006799af4"; approvalID != want {
		t.Fatalf("ApprovalID() = %s, want %s", approvalID, want)
	}
	effectKey, err := EffectKey("run-123", proposalHash, "primary")
	if err != nil {
		t.Fatal(err)
	}
	if want := "91f0328308f2bc6663160cfc0f4cadbb44fa6f26c677cd3c30cc61073f89bddc"; effectKey != want {
		t.Fatalf("EffectKey() = %s, want %s", effectKey, want)
	}
}

func TestProposalRejectsPlaintextSecretsAndKeepsReferencesInHash(t *testing.T) {
	base := Proposal{
		ToolName:                 "notify",
		ToolRevision:             "v1",
		ToolSchemaHash:           "schema-v1",
		RiskLevel:                RiskL1,
		TargetScope:              map[string]any{"channel": "security"},
		PolicyHash:               "policy-v1",
		RuntimeCompatibilityHash: "runtime-v1",
	}

	for name, arguments := range map[string]any{
		"api key":       map[string]any{"api_key": "plaintext-value"},
		"authorization": map[string]any{"headers": map[string]any{"Authorization": "Bearer plaintext-token"}},
		"private key":   map[string]any{"payload": "-----BEGIN PRIVATE KEY-----\nplaintext\n-----END PRIVATE KEY-----"},
		"dsn":           map[string]any{"dsn": "user:password@tcp(db:3306)/sentinel"},
	} {
		t.Run(name, func(t *testing.T) {
			proposal := base
			proposal.ArgumentsWithSecretRefs = arguments
			if _, err := ProposalHash(proposal); !errors.Is(err, ErrPlaintextSecret) {
				t.Fatalf("ProposalHash() error = %v, want ErrPlaintextSecret", err)
			}
		})
	}

	first := base
	first.ArgumentsWithSecretRefs = map[string]any{"api_key": "env:NOTIFY_API_KEY_A"}
	second := base
	second.ArgumentsWithSecretRefs = map[string]any{"api_key": "env:NOTIFY_API_KEY_B"}
	firstHash, err := ProposalHash(first)
	if err != nil {
		t.Fatal(err)
	}
	secondHash, err := ProposalHash(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstHash == secondHash {
		t.Fatal("distinct Secret references must participate in proposal identity")
	}

	redactor := NewRedactor()
	firstView, err := redactor.RedactJSON(first.ArgumentsWithSecretRefs)
	if err != nil {
		t.Fatal(err)
	}
	secondView, err := redactor.RedactJSON(second.ArgumentsWithSecretRefs)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstView) != string(secondView) || strings.Contains(string(firstView), "NOTIFY_API_KEY") {
		t.Fatalf("display redaction must be separate from Hash input: first=%s second=%s", firstView, secondView)
	}
}

func TestFrozenProposalIsImmutable(t *testing.T) {
	arguments := map[string]any{"ip": "192.0.2.1"}
	proposal := Proposal{
		ToolName:                 "block_ip",
		ToolRevision:             "v1",
		ToolSchemaHash:           "schema-v1",
		RiskLevel:                RiskL2,
		ArgumentsWithSecretRefs:  arguments,
		TargetScope:              map[string]any{"asset_id": "edge-1"},
		PolicyHash:               "policy-v1",
		RuntimeCompatibilityHash: "runtime-v1",
	}
	frozen, err := FreezeProposal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	beforeHash := frozen.Hash()
	beforeJSON := frozen.CanonicalJSON()
	arguments["ip"] = "198.51.100.1"
	if frozen.Hash() != beforeHash || string(frozen.CanonicalJSON()) != string(beforeJSON) {
		t.Fatal("frozen Proposal changed after caller mutated its input")
	}
	beforeJSON[0] = 'x'
	if string(frozen.CanonicalJSON()) == string(beforeJSON) {
		t.Fatal("CanonicalJSON exposed mutable internal storage")
	}
}

func TestRiskLevelFailClosed(t *testing.T) {
	for _, level := range []RiskLevel{RiskL0, RiskL1, RiskL2} {
		if err := level.Validate(); err != nil {
			t.Fatalf("%s.Validate() error = %v", level, err)
		}
	}
	if err := RiskLevel("unknown").Validate(); err == nil {
		t.Fatal("unknown risk level was accepted")
	}
}
