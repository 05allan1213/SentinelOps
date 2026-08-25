package ops

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	opsv1 "SentinelOps/api/ops/v1"
	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"

	"github.com/gogf/gf/v2/frame/g"
)

type reconciliationRepository interface {
	ListUnknownEffects(context.Context, int) ([]mysql.AgentEffect, error)
	AdminResolveEffect(context.Context, workflow.AdminResolveEffectInput) error
	AdminAcceptUnknownAndCancel(context.Context, workflow.AdminAcceptUnknownInput) error
}

// ListUnknownEffects 返回当前 admin 可见的 unknown Effect 摘要。
func (c *ControllerV1) ListUnknownEffects(ctx context.Context, req *opsv1.ListUnknownEffectsReq) (*opsv1.ListUnknownEffectsRes, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	store, err := c.reconciliationRepository(ctx)
	if err != nil {
		return nil, err
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	rows, err := store.ListUnknownEffects(ctx, limit)
	if err != nil {
		writeReconciliationHTTPStatus(ctx, err)
		return nil, err
	}
	items := make([]opsv1.UnknownEffectItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, toUnknownEffectItem(row))
	}
	return &opsv1.ListUnknownEffectsRes{Items: items}, nil
}

// ResolveEffect 只提交 admin 的证据与结论，不调用 Action/Tool/外部 endpoint。
func (c *ControllerV1) ResolveEffect(ctx context.Context, req *opsv1.ResolveEffectReq) (*opsv1.ResolveEffectRes, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	identity, err := policy.IdentityFromContext(ctx)
	if err != nil {
		return nil, err
	}
	store, err := c.reconciliationRepository(ctx)
	if err != nil {
		return nil, err
	}
	if !validReconciliationResolution(req.Resolution) {
		return nil, workflow.ErrEffectResolutionInvalid
	}
	if err := store.AdminResolveEffect(ctx, workflow.AdminResolveEffectInput{
		EffectID: req.ID, Resolution: req.Resolution, ResponseRedacted: jsonText(req.Response),
		ExternalReference: req.ExternalReference, EvidenceRedacted: jsonText(req.Evidence),
		Owner: identity.UserID, LeaseDuration: 5 * time.Minute,
	}); err != nil {
		writeReconciliationHTTPStatus(ctx, err)
		return nil, err
	}
	return &opsv1.ResolveEffectRes{}, nil
}

// AcceptUnknownEffect 记录 admin 风险接受并通过 P08 完成 primitive 取消 Run。
func (c *ControllerV1) AcceptUnknownEffect(ctx context.Context, req *opsv1.AcceptUnknownEffectReq) (*opsv1.AcceptUnknownEffectRes, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	identity, err := policy.IdentityFromContext(ctx)
	if err != nil {
		return nil, err
	}
	store, err := c.reconciliationRepository(ctx)
	if err != nil {
		return nil, err
	}
	if err := store.AdminAcceptUnknownAndCancel(ctx, workflow.AdminAcceptUnknownInput{
		RunID: req.RunID, EffectID: req.ID, Owner: identity.UserID, LeaseDuration: 5 * time.Minute,
		EvidenceRedacted: jsonText(req.Evidence), Reason: req.Reason,
	}); err != nil {
		writeReconciliationHTTPStatus(ctx, err)
		return nil, err
	}
	return &opsv1.AcceptUnknownEffectRes{}, nil
}

func (c *ControllerV1) reconciliationRepository(ctx context.Context) (reconciliationRepository, error) {
	if c.reconciliation != nil {
		return c.reconciliation, nil
	}
	db, err := mysql.DB(ctx)
	if err != nil {
		return nil, err
	}
	return workflow.NewGORMStore(db), nil
}

func toUnknownEffectItem(effect mysql.AgentEffect) opsv1.UnknownEffectItem {
	evidence := json.RawMessage(nil)
	if effect.ResolutionEvidenceRedacted != nil && json.Valid([]byte(*effect.ResolutionEvidenceRedacted)) {
		evidence = json.RawMessage(*effect.ResolutionEvidenceRedacted)
	}
	return opsv1.UnknownEffectItem{ID: effect.ID, RunID: effect.RunID, ToolName: effect.ToolName, EffectStep: effect.EffectStep, EffectType: effect.EffectType, Status: effect.Status, Version: effect.Version, Evidence: evidence, ReconcileAttempts: effect.ReconciliationAttempts}
}

func validReconciliationResolution(value string) bool {
	return value == workflow.EffectResolutionExecuted || value == workflow.EffectResolutionNotExecuted || value == workflow.EffectResolutionStillUnknown
}

func jsonText(raw json.RawMessage) string {
	if len(raw) == 0 || !json.Valid(raw) {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func reconciliationHTTPStatus(err error) int {
	switch {
	case errors.Is(err, policy.ErrForbidden), errors.Is(err, policy.ErrUnauthenticated):
		return http.StatusForbidden
	case errors.Is(err, workflow.ErrEffectStateConflict), errors.Is(err, workflow.ErrRunCASConflict), errors.Is(err, workflow.ErrEffectResolutionInvalid), errors.Is(err, workflow.ErrEffectReconciliationDenied):
		return http.StatusConflict
	default:
		return 0
	}
}

func writeReconciliationHTTPStatus(ctx context.Context, err error) {
	if status := reconciliationHTTPStatus(err); status != 0 {
		if request := g.RequestFromCtx(ctx); request != nil {
			request.Response.WriteStatus(status)
		}
	}
}
