package policy

import (
	"context"
	"errors"
	"testing"
)

func TestRoleMatrix(t *testing.T) {
	t.Parallel()

	type expectation struct {
		permission Permission
		resource   Resource
		allowed    map[Role]bool
	}
	allMutatingRoles := map[Role]bool{RoleOperator: true, RoleApprover: true, RoleAdmin: true}
	tests := []expectation{
		{PermissionViewScoped, Resource{OwnerID: "subject"}, map[Role]bool{RoleViewer: true, RoleOperator: true, RoleApprover: true, RoleAdmin: true}},
		{PermissionCreateReadOnlyRun, Resource{}, map[Role]bool{RoleViewer: true, RoleOperator: true, RoleApprover: true, RoleAdmin: true}},
		{PermissionWriteOwnFeedback, Resource{OwnerID: "subject"}, allMutatingRoles},
		{PermissionProposeMutation, Resource{}, allMutatingRoles},
		{PermissionExecutePreauthorizedL1, Resource{Preauthorized: true, StaticL1Allowed: true}, allMutatingRoles},
		{PermissionDecideProposal, Resource{OwnerID: "other"}, map[Role]bool{RoleApprover: true, RoleAdmin: true}},
		{PermissionBusinessWrite, Resource{}, allMutatingRoles},
		{PermissionManageUsersPolicyGates, Resource{}, map[Role]bool{RoleAdmin: true}},
	}

	for _, test := range tests {
		test := test
		for _, role := range []Role{RoleViewer, RoleOperator, RoleApprover, RoleAdmin} {
			role := role
			t.Run(string(test.permission)+"/"+string(role), func(t *testing.T) {
				identity := Identity{UserID: "subject", Role: role, Scope: Scope{UserID: "subject"}}
				err := Authorize(WithIdentity(context.Background(), identity), test.permission, test.resource)
				if got, want := err == nil, test.allowed[role]; got != want {
					t.Fatalf("Authorize() allowed=%v, want %v, err=%v", got, want, err)
				}
			})
		}
	}

	for _, role := range []Role{RoleViewer, RoleOperator, RoleApprover, RoleAdmin} {
		ctx := WithIdentity(context.Background(), Identity{UserID: "same", Role: role, Scope: Scope{UserID: "same"}})
		if err := Authorize(ctx, PermissionDecideProposal, Resource{OwnerID: "same"}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("role %q self-approved L2 proposal: %v", role, err)
		}
	}

	operator := WithIdentity(context.Background(), Identity{UserID: "operator", Role: RoleOperator, Scope: Scope{UserID: "operator"}})
	if err := Authorize(operator, PermissionExecutePreauthorizedL1, Resource{Preauthorized: true}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("preauthorized L1 bypassed static upper bound: %v", err)
	}
	viewer := WithIdentity(context.Background(), Identity{UserID: "viewer", Role: RoleViewer, Scope: Scope{UserID: "viewer"}})
	if err := Authorize(viewer, PermissionViewScoped, Resource{OwnerID: "other"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer read outside Scope: %v", err)
	}
	admin := WithIdentity(context.Background(), Identity{UserID: "admin", Role: RoleAdmin, Scope: Scope{All: true}})
	if err := Authorize(admin, PermissionViewScoped, Resource{OwnerID: "other"}); err != nil {
		t.Fatalf("admin could not read authorized all Scope: %v", err)
	}
	approverIdentity, err := NewIdentity("approver", "", "approver")
	if err != nil || approverIdentity.Scope.All {
		t.Fatalf("approver unexpectedly received global Scope: identity=%+v err=%v", approverIdentity, err)
	}
	adminIdentity, err := NewIdentity("admin", "", "admin")
	if err != nil || !adminIdentity.Scope.All {
		t.Fatalf("admin did not receive global Scope: identity=%+v err=%v", adminIdentity, err)
	}
}

func TestAuthDisabledRoleMatrixIsReadOnly(t *testing.T) {
	t.Parallel()
	ctx := WithIdentity(context.Background(), DisabledIdentity())
	for _, permission := range []Permission{PermissionProposeMutation, PermissionExecutePreauthorizedL1, PermissionDecideProposal, PermissionBusinessWrite, PermissionManageUsersPolicyGates} {
		if err := Authorize(ctx, permission, Resource{OwnerID: "other", Preauthorized: true, StaticL1Allowed: true}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("auth-disabled identity unexpectedly received %q: %v", permission, err)
		}
	}
	for _, permission := range []Permission{PermissionViewScoped, PermissionCreateReadOnlyRun} {
		if err := Authorize(ctx, permission, Resource{OwnerID: DisabledUserID}); err != nil {
			t.Fatalf("auth-disabled viewer lost %q: %v", permission, err)
		}
	}
}
