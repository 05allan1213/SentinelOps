package ops

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	opsv1 "SentinelOps/api/ops/v1"
	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"

	"github.com/gogf/gf/v2/frame/g"
)

type approvalRepository interface {
	ListPendingApprovals(context.Context, int) ([]mysql.AgentApproval, error)
	GetPendingApproval(context.Context, string) (*mysql.AgentApproval, error)
	DecideApprovalAndWakeRun(context.Context, workflow.DecideApprovalInput) (*mysql.AgentApproval, error)
}

type approvalDecisionRequest struct {
	ProposalHash    string
	ExpectedVersion uint64
	Reason          string
}

// ListApprovals 返回当前审批人可操作的他人 pending Approval。
func (c *ControllerV1) ListApprovals(ctx context.Context, req *opsv1.ListApprovalsReq) (*opsv1.ListApprovalsRes, error) {
	store, err := c.approvalRepository(ctx)
	if err != nil {
		return nil, err
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	items, err := store.ListPendingApprovals(ctx, limit)
	if err != nil {
		writeApprovalHTTPStatus(ctx, err)
		return nil, err
	}
	response := make([]opsv1.ApprovalItem, 0, len(items))
	for index := range items {
		response = append(response, toApprovalItem(items[index]))
	}
	return &opsv1.ListApprovalsRes{Items: response}, nil
}

// GetApproval 返回一个可操作的 pending Approval；preparing 与不存在统一为 404。
func (c *ControllerV1) GetApproval(ctx context.Context, req *opsv1.GetApprovalReq) (*opsv1.GetApprovalRes, error) {
	store, err := c.approvalRepository(ctx)
	if err != nil {
		return nil, err
	}
	approval, err := store.GetPendingApproval(ctx, req.ID)
	if err != nil {
		writeApprovalHTTPStatus(ctx, err)
		return nil, err
	}
	return &opsv1.GetApprovalRes{Item: toApprovalItem(*approval)}, nil
}

// ApproveApproval 使用客户端回传的 version 与 proposal_hash 执行 CAS。
func (c *ControllerV1) ApproveApproval(ctx context.Context, req *opsv1.ApproveApprovalReq) (*opsv1.ApproveApprovalRes, error) {
	approval, err := c.decideApproval(ctx, req.ID, approvalDecisionRequest{
		ProposalHash: req.ProposalHash, ExpectedVersion: req.ExpectedVersion, Reason: req.Reason,
	}, workflow.ApprovalStatusApproved)
	if err != nil {
		return nil, err
	}
	return &opsv1.ApproveApprovalRes{Item: toApprovalItem(*approval)}, nil
}

// RejectApproval 使用客户端回传的 version 与 proposal_hash 执行 CAS。
func (c *ControllerV1) RejectApproval(ctx context.Context, req *opsv1.RejectApprovalReq) (*opsv1.RejectApprovalRes, error) {
	approval, err := c.decideApproval(ctx, req.ID, approvalDecisionRequest{
		ProposalHash: req.ProposalHash, ExpectedVersion: req.ExpectedVersion, Reason: req.Reason,
	}, workflow.ApprovalStatusRejected)
	if err != nil {
		return nil, err
	}
	return &opsv1.RejectApprovalRes{Item: toApprovalItem(*approval)}, nil
}

func (c *ControllerV1) decideApproval(ctx context.Context, approvalID string, request approvalDecisionRequest, decision string) (*mysql.AgentApproval, error) {
	if err := validateApprovalDecisionRequest(request); err != nil {
		return nil, err
	}
	store, err := c.approvalRepository(ctx)
	if err != nil {
		return nil, err
	}
	approval, err := store.DecideApprovalAndWakeRun(ctx, workflow.DecideApprovalInput{
		ApprovalID: approvalID, ProposalHash: request.ProposalHash,
		ExpectedVersion: request.ExpectedVersion, Decision: decision, Reason: request.Reason,
	})
	if err != nil {
		writeApprovalHTTPStatus(ctx, err)
		return nil, err
	}
	return approval, nil
}

func (c *ControllerV1) approvalRepository(ctx context.Context) (approvalRepository, error) {
	if c.approvals != nil {
		return c.approvals, nil
	}
	db, err := mysql.DB(ctx)
	if err != nil {
		return nil, err
	}
	return workflow.NewGORMStore(db), nil
}

func validateApprovalDecisionRequest(request approvalDecisionRequest) error {
	if request.ProposalHash == "" {
		return workflow.ErrApprovalProposalMismatch
	}
	if request.ExpectedVersion == 0 {
		return workflow.ErrApprovalVersionConflict
	}
	return nil
}

func approvalHTTPStatus(err error) int {
	switch {
	case errors.Is(err, workflow.ErrApprovalNotFound):
		return http.StatusNotFound
	case errors.Is(err, policy.ErrForbidden), errors.Is(err, policy.ErrUnauthenticated):
		return http.StatusForbidden
	case errors.Is(err, workflow.ErrApprovalAlreadyDecided),
		errors.Is(err, workflow.ErrApprovalExpired),
		errors.Is(err, workflow.ErrApprovalVersionConflict),
		errors.Is(err, workflow.ErrApprovalProposalMismatch),
		errors.Is(err, workflow.ErrApprovalIdentityMismatch),
		errors.Is(err, workflow.ErrApprovalNotExpired),
		errors.Is(err, workflow.ErrRunCASConflict):
		return http.StatusConflict
	default:
		return 0
	}
}

func writeApprovalHTTPStatus(ctx context.Context, err error) {
	if status := approvalHTTPStatus(err); status != 0 {
		if request := g.RequestFromCtx(ctx); request != nil {
			request.Response.WriteHeader(status)
		}
	}
}

func toApprovalItem(approval mysql.AgentApproval) opsv1.ApprovalItem {
	proposal := json.RawMessage(approval.ProposalJSONRedacted)
	if !json.Valid(proposal) {
		proposal = json.RawMessage(`null`)
	}
	item := opsv1.ApprovalItem{
		ID: approval.ID, RunID: approval.RunID, ToolName: approval.ToolName,
		ToolRevision: approval.ToolRevision, RiskLevel: approval.RiskLevel,
		Proposal: proposal, ProposalHash: approval.ProposalHash,
		RequestedBy: approval.RequestedBy, Status: approval.Status, Version: approval.Version,
		PublishedAt: formatApprovalTime(approval.PublishedAt),
		ExpiresAt:   formatApprovalTime(approval.ExpiresAt),
		DecidedAt:   formatApprovalTime(approval.DecidedAt),
	}
	if approval.DecidedBy != nil {
		item.DecidedBy = *approval.DecidedBy
	}
	if approval.DecisionReason != nil {
		item.DecisionReason = *approval.DecisionReason
	}
	return item
}

func formatApprovalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
