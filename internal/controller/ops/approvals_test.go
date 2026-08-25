package ops

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	opsv1 "SentinelOps/api/ops/v1"
	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
)

func TestApprovalConflictErrorsMapToHTTP409(t *testing.T) {
	for _, err := range []error{
		workflow.ErrApprovalAlreadyDecided,
		workflow.ErrApprovalExpired,
		workflow.ErrApprovalVersionConflict,
		workflow.ErrApprovalProposalMismatch,
		workflow.ErrApprovalIdentityMismatch,
	} {
		if got := approvalHTTPStatus(fmt.Errorf("wrapped: %w", err)); got != http.StatusConflict {
			t.Fatalf("error %v mapped to %d, want 409", err, got)
		}
	}
}

func TestApprovalAPIForwardsVersionHashAndReturnsDecision(t *testing.T) {
	row := mysql.AgentApproval{
		ID: "approval", RunID: "run", ProposalJSONRedacted: `{"safe":true}`,
		ProposalHash: "stable-hash", RequestedBy: "requester",
		Status: workflow.ApprovalStatusPending, Version: 7,
	}
	repository := &approvalRepositoryFake{items: []mysql.AgentApproval{row}, item: &row, decided: &row}
	controller := &ControllerV1{approvals: repository}
	list, err := controller.ListApprovals(context.Background(), &opsv1.ListApprovalsReq{Limit: 20})
	if err != nil || len(list.Items) != 1 || list.Items[0].ProposalHash != row.ProposalHash {
		t.Fatalf("list response=%#v err=%v", list, err)
	}
	detail, err := controller.GetApproval(context.Background(), &opsv1.GetApprovalReq{ID: row.ID})
	if err != nil || detail.Item.ProposalHash != row.ProposalHash {
		t.Fatalf("detail response=%#v err=%v", detail, err)
	}
	response, err := controller.ApproveApproval(context.Background(), &opsv1.ApproveApprovalReq{
		ID: row.ID, ProposalHash: row.ProposalHash, ExpectedVersion: row.Version, Reason: "reviewed",
	})
	if err != nil || response.Item.ID != row.ID {
		t.Fatalf("approve response=%#v err=%v", response, err)
	}
	if repository.input.ApprovalID != row.ID || repository.input.ProposalHash != row.ProposalHash || repository.input.ExpectedVersion != row.Version || repository.input.Decision != workflow.ApprovalStatusApproved {
		t.Fatalf("decision input=%#v", repository.input)
	}
	rejected, err := controller.RejectApproval(context.Background(), &opsv1.RejectApprovalReq{
		ID: row.ID, ProposalHash: row.ProposalHash, ExpectedVersion: row.Version, Reason: "rejected",
	})
	if err != nil || rejected.Item.ID != row.ID || repository.input.Decision != workflow.ApprovalStatusRejected {
		t.Fatalf("reject response=%#v input=%#v err=%v", rejected, repository.input, err)
	}
}

func TestApprovalVisibilityAndAuthorizationErrorsHaveStableHTTPStatus(t *testing.T) {
	if got := approvalHTTPStatus(workflow.ErrApprovalNotFound); got != http.StatusNotFound {
		t.Fatalf("not found mapped to %d", got)
	}
	if got := approvalHTTPStatus(policy.ErrForbidden); got != http.StatusForbidden {
		t.Fatalf("forbidden mapped to %d", got)
	}
}

func TestApprovalItemAlwaysReturnsProposalHash(t *testing.T) {
	item := toApprovalItem(mysql.AgentApproval{ID: "approval", ProposalHash: "stable-hash", Status: workflow.ApprovalStatusPending})
	if item.ID != "approval" || item.ProposalHash != "stable-hash" || item.Status != workflow.ApprovalStatusPending {
		t.Fatalf("Approval DTO=%#v", item)
	}
}

func TestApprovalDecisionValidationRejectsMissingCASFields(t *testing.T) {
	for _, input := range []approvalDecisionRequest{
		{ProposalHash: "hash", ExpectedVersion: 0},
		{ProposalHash: "", ExpectedVersion: 1},
	} {
		if err := validateApprovalDecisionRequest(input); err == nil {
			t.Fatalf("invalid decision request accepted: %#v", input)
		}
	}
}

func TestApprovalDecisionSurfaceHasNoEffectExecution(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate controller test source")
	}
	source, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "approvals.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"internal/ai/effects", "internal/ai/tools", ".Execute("} {
		if strings.Contains(string(source), forbidden) {
			t.Fatalf("Approval decision surface contains forbidden execution path %q", forbidden)
		}
	}
}

type approvalRepositoryFake struct {
	items   []mysql.AgentApproval
	item    *mysql.AgentApproval
	decided *mysql.AgentApproval
	input   workflow.DecideApprovalInput
}

func (f *approvalRepositoryFake) ListPendingApprovals(context.Context, int) ([]mysql.AgentApproval, error) {
	return f.items, nil
}

func (f *approvalRepositoryFake) GetPendingApproval(context.Context, string) (*mysql.AgentApproval, error) {
	return f.item, nil
}

func (f *approvalRepositoryFake) DecideApprovalAndWakeRun(_ context.Context, input workflow.DecideApprovalInput) (*mysql.AgentApproval, error) {
	f.input = input
	return f.decided, nil
}
