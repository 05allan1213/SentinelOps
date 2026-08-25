package v1

import (
	"encoding/json"

	"github.com/gogf/gf/v2/frame/g"
)

// UnknownEffectItem 是 admin 对账所需的脱敏 Effect 摘要。
type UnknownEffectItem struct {
	ID                string          `json:"id"`
	RunID             string          `json:"run_id"`
	ToolName          string          `json:"tool_name"`
	EffectStep        string          `json:"effect_step"`
	EffectType        string          `json:"effect_type"`
	Status            string          `json:"status"`
	Version           uint64          `json:"version"`
	Evidence          json.RawMessage `json:"resolution_evidence,omitempty"`
	ReconcileAttempts uint            `json:"reconciliation_attempts"`
}

type ListUnknownEffectsReq struct {
	g.Meta `path:"/ops/v1/effects/unknown" method:"get"`
	Limit  int `p:"limit" d:"20"`
}

type ListUnknownEffectsRes struct {
	Items []UnknownEffectItem `json:"items"`
}

type ResolveEffectReq struct {
	g.Meta            `path:"/ops/v1/effects/{id}/resolve" method:"post"`
	ID                string          `p:"id" v:"required"`
	Resolution        string          `json:"resolution" v:"required"`
	Evidence          json.RawMessage `json:"evidence"`
	Response          json.RawMessage `json:"response"`
	ExternalReference string          `json:"external_reference"`
}

type ResolveEffectRes struct{}

type AcceptUnknownEffectReq struct {
	g.Meta   `path:"/ops/v1/effects/{id}/accept-unknown" method:"post"`
	ID       string          `p:"id" v:"required"`
	RunID    string          `json:"run_id" v:"required"`
	Evidence json.RawMessage `json:"evidence"`
	Reason   string          `json:"reason" v:"required"`
}

type AcceptUnknownEffectRes struct{}
