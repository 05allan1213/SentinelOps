package v1

import (
	"encoding/json"

	"github.com/gogf/gf/v2/frame/g"
)

// ApprovalItem 是外部可见的 pending 或刚完成决策的审批事实。
type ApprovalItem struct {
	ID             string          `json:"id"`
	RunID          string          `json:"run_id"`
	ToolName       string          `json:"tool_name"`
	ToolRevision   string          `json:"tool_revision"`
	RiskLevel      string          `json:"risk_level"`
	Proposal       json.RawMessage `json:"proposal"`
	ProposalHash   string          `json:"proposal_hash"`
	RequestedBy    string          `json:"requested_by"`
	DecidedBy      string          `json:"decided_by,omitempty"`
	Status         string          `json:"status"`
	Version        uint64          `json:"version"`
	DecisionReason string          `json:"decision_reason,omitempty"`
	PublishedAt    string          `json:"published_at,omitempty"`
	ExpiresAt      string          `json:"expires_at,omitempty"`
	DecidedAt      string          `json:"decided_at,omitempty"`
}

type ListApprovalsReq struct {
	g.Meta `path:"/ops/v1/approvals" method:"get"`
	Limit  int `p:"limit" d:"20"`
}

type ListApprovalsRes struct {
	Items []ApprovalItem `json:"items"`
}

type GetApprovalReq struct {
	g.Meta `path:"/ops/v1/approvals/{id}" method:"get"`
	ID     string `p:"id" v:"required"`
}

type GetApprovalRes struct {
	Item ApprovalItem `json:"item"`
}

type ApproveApprovalReq struct {
	g.Meta          `path:"/ops/v1/approvals/{id}/approve" method:"post"`
	ID              string `p:"id" v:"required"`
	ProposalHash    string `json:"proposal_hash" v:"required|length:64,64"`
	ExpectedVersion uint64 `json:"version" v:"required|min:1"`
	Reason          string `json:"reason"`
}

type ApproveApprovalRes struct {
	Item ApprovalItem `json:"item"`
}

type RejectApprovalReq struct {
	g.Meta          `path:"/ops/v1/approvals/{id}/reject" method:"post"`
	ID              string `p:"id" v:"required"`
	ProposalHash    string `json:"proposal_hash" v:"required|length:64,64"`
	ExpectedVersion uint64 `json:"version" v:"required|min:1"`
	Reason          string `json:"reason"`
}

type RejectApprovalRes struct {
	Item ApprovalItem `json:"item"`
}
