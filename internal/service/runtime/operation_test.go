package runtime

import (
	"errors"
	"strings"
	"testing"

	runtimev1 "SentinelOps/api/runtime/v1"
	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
)

func TestRecoveryAdminOnly(t *testing.T) {
	request := runtimev1.RecoveryCommandRequest{Action: runtimev1.RecoveryActionReplay, IdempotencyKey: "runtime-operation-key-0001", Reason: "recover", ExpectedGeneration: 1}
	for _, role := range []policy.Role{policy.RoleViewer, policy.RoleOperator, policy.RoleApprover} {
		if _, err := ValidateRecoveryRequest(runtimeIdentity(string(role), role), "run-c04", request); !errors.Is(err, ErrRuntimeForbidden) {
			t.Fatalf("role %s error = %v", role, err)
		}
	}
	input, err := ValidateRecoveryRequest(runtimeIdentity("admin-context", policy.RoleAdmin), "run-c04", request)
	if err != nil {
		t.Fatalf("admin request: %v", err)
	}
	if input.RunID != "run-c04" || input.Action != workflow.OperationActionReplay || input.IdempotencyKey != request.IdempotencyKey {
		t.Fatalf("validated input = %#v", input)
	}
}

func TestRecoveryRequestValidationAndErrorMapping(t *testing.T) {
	ctx := runtimeIdentity("admin-context", policy.RoleAdmin)
	valid := runtimev1.RecoveryCommandRequest{Action: runtimev1.RecoveryActionResume, IdempotencyKey: "runtime-operation-key-0001", Reason: "recover", ExpectedGeneration: 1}
	if _, err := ValidateRecoveryRequest(ctx, "run-c04", valid); err != nil {
		t.Fatalf("valid request: %v", err)
	}
	invalid := []runtimev1.RecoveryCommandRequest{
		{Action: runtimev1.RecoveryAction("unlock"), IdempotencyKey: valid.IdempotencyKey, Reason: valid.Reason, ExpectedGeneration: 1},
		{Action: runtimev1.RecoveryActionReplay, IdempotencyKey: "short", Reason: valid.Reason, ExpectedGeneration: 1},
		{Action: runtimev1.RecoveryActionReplay, IdempotencyKey: valid.IdempotencyKey, Reason: " ", ExpectedGeneration: 1},
		{Action: runtimev1.RecoveryActionRestore, IdempotencyKey: valid.IdempotencyKey, Reason: valid.Reason, ExpectedGeneration: 1},
		{Action: runtimev1.RecoveryActionCancel, IdempotencyKey: valid.IdempotencyKey, Reason: valid.Reason, ExpectedCompatibilityHash: strings.Repeat("A", 64)},
	}
	for index, request := range invalid {
		if _, err := ValidateRecoveryRequest(ctx, "run-c04", request); !errors.Is(err, ErrRuntimeInvalidFilter) {
			t.Fatalf("invalid[%d] error = %v", index, err)
		}
	}
	for _, test := range []struct {
		input error
		want  error
	}{
		{workflow.ErrOperationConflict, ErrRuntimeOperationConflict},
		{workflow.ErrOperationIdempotencyConflict, ErrRuntimeIdempotencyConflict},
		{workflow.ErrOperationPrecondition, ErrRuntimePrecondition},
		{workflow.ErrInvalidOperationInput, ErrRuntimeInvalidFilter},
	} {
		if got := MapRecoveryError(test.input); !errors.Is(got, test.want) {
			t.Fatalf("map %v = %v, want %v", test.input, got, test.want)
		}
	}
}
