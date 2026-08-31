package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"

	"SentinelOps/internal/ai/policy"
)

func runtimeIdentity(userID string, role policy.Role) context.Context {
	identity := policy.Identity{
		UserID: userID,
		Role:   role,
		Scope:  policy.Scope{UserID: userID},
	}
	if role == policy.RoleAdmin {
		identity.Scope = policy.Scope{All: true}
	}
	return policy.WithIdentity(context.Background(), identity)
}

func TestRuntimeReadScopeMatrix(t *testing.T) {
	for _, role := range []policy.Role{policy.RoleViewer, policy.RoleOperator, policy.RoleApprover} {
		role := role
		t.Run(string(role)+" own metadata", func(t *testing.T) {
			if err := AuthorizeRun(runtimeIdentity("owner", role), "owner"); err != nil {
				t.Fatalf("AuthorizeRun() error=%v", err)
			}
		})
		t.Run(string(role)+" other metadata", func(t *testing.T) {
			err := AuthorizeRun(runtimeIdentity("caller", role), "owner")
			if !errors.Is(err, ErrRuntimeForbidden) {
				t.Fatalf("AuthorizeRun() error=%v, want ErrRuntimeForbidden", err)
			}
			if containsRuntimeSensitiveText(err, "owner") {
				t.Fatalf("cross-scope error leaked owner: %q", err)
			}
		})
	}

	if err := AuthorizeRun(runtimeIdentity("admin", policy.RoleAdmin), "owner"); err != nil {
		t.Fatalf("admin global metadata read error=%v", err)
	}
	if err := AuthorizeRun(runtimeIdentity("admin", policy.RoleAdmin), ""); !errors.Is(err, ErrRuntimeNotFound) {
		t.Fatalf("missing Run error=%v, want ErrRuntimeNotFound", err)
	}
}

func TestRuntimeRecoveryAdminOnly(t *testing.T) {
	for _, role := range []policy.Role{policy.RoleViewer, policy.RoleOperator, policy.RoleApprover} {
		err := RequireRuntimeAdmin(runtimeIdentity(string(role), role))
		if !errors.Is(err, ErrRuntimeForbidden) {
			t.Fatalf("role %q recovery error=%v, want ErrRuntimeForbidden", role, err)
		}
	}
	if err := RequireRuntimeAdmin(runtimeIdentity("admin", policy.RoleAdmin)); err != nil {
		t.Fatalf("admin recovery error=%v", err)
	}
}

func TestRuntimeContentExpansionRequiresResourcePermission(t *testing.T) {
	for _, kind := range []ContentKind{
		ContentKindHistory,
		ContentKindEvidenceQuote,
		ContentKindTracePrompt,
		ContentKindTraceResponse,
		ContentKindToolArguments,
	} {
		if err := AuthorizeRuntimeContent(runtimeIdentity("owner", policy.RoleViewer), "owner", kind); err != nil {
			t.Fatalf("own %q expansion error=%v", kind, err)
		}
		if err := AuthorizeRuntimeContent(runtimeIdentity("admin", policy.RoleAdmin), "owner", kind); err != nil {
			t.Fatalf("admin %q expansion error=%v", kind, err)
		}
		err := AuthorizeRuntimeContent(runtimeIdentity("other", policy.RoleApprover), "owner", kind)
		if !errors.Is(err, ErrRuntimeContentExpansionDenied) {
			t.Fatalf("cross-scope %q expansion error=%v, want expansion denied", kind, err)
		}
	}

	if err := AuthorizeRuntimeContent(runtimeIdentity("owner", policy.RoleViewer), "owner", ContentKind("future_raw_value")); !errors.Is(err, ErrRuntimeInvalidFilter) {
		t.Fatalf("unknown content kind error=%v, want ErrRuntimeInvalidFilter", err)
	}
	if _, err := ParseContentKind(" history "); !errors.Is(err, ErrRuntimeInvalidFilter) {
		t.Fatalf("whitespace-padded content kind error=%v, want ErrRuntimeInvalidFilter", err)
	}
}

func TestAuthDisabledRuntimeIsReadOnly(t *testing.T) {
	ctx := policy.WithIdentity(context.Background(), policy.DisabledIdentity())
	if err := AuthorizeRun(ctx, policy.DisabledUserID); err != nil {
		t.Fatalf("auth-disabled own metadata error=%v", err)
	}
	if err := AuthorizeRun(ctx, "other"); !errors.Is(err, ErrRuntimeForbidden) {
		t.Fatalf("auth-disabled cross-scope metadata error=%v", err)
	}
	if err := AuthorizeRuntimeContent(ctx, policy.DisabledUserID, ContentKindHistory); !errors.Is(err, ErrRuntimeContentExpansionDenied) {
		t.Fatalf("auth-disabled content error=%v", err)
	}
	if err := RequireRuntimeAdmin(ctx); !errors.Is(err, ErrRuntimeForbidden) {
		t.Fatalf("auth-disabled recovery error=%v", err)
	}
}

func TestRuntimeErrorCodeMapIsStableAndRedacted(t *testing.T) {
	tests := []struct {
		err  error
		code string
	}{
		{ErrRuntimeNotFound, "RUNTIME_NOT_FOUND"},
		{ErrRuntimeForbidden, "RUNTIME_FORBIDDEN"},
		{ErrRuntimeInvalidFilter, "RUNTIME_INVALID_FILTER"},
		{ErrRuntimeContentExpansionDenied, "RUNTIME_FORBIDDEN"},
		{ErrRuntimeOperationConflict, "RUNTIME_OPERATION_CONFLICT"},
		{ErrRuntimeIdempotencyConflict, "RUNTIME_IDEMPOTENCY_CONFLICT"},
		{ErrRuntimePrecondition, "RUNTIME_PRECONDITION_FAILED"},
		{errors.New("password=plaintext-runtime-secret"), "RUNTIME_INTERNAL"},
	}
	for _, test := range tests {
		if got := RuntimeErrorCode(test.err); got != test.code {
			t.Fatalf("RuntimeErrorCode(%v)=%q, want %q", test.err, got, test.code)
		}
	}
}

func containsRuntimeSensitiveText(err error, value string) bool {
	return err != nil && (strings.Contains(err.Error(), value) || policy.NewRedactor().RedactText(err.Error()) != err.Error())
}
