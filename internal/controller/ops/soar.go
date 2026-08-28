// Package ops AI 智能运维控制器
package ops

import (
	"context"

	soarv1 "SentinelOps/api/ops/v1"
	"SentinelOps/internal/ai/ops/engine"
	"SentinelOps/internal/ai/policy"
	dao "SentinelOps/internal/dao/mysql"

	"github.com/gogf/gf/v2/errors/gerror"
)

type ControllerV1 struct {
	approvals      approvalRepository
	reconciliation reconciliationRepository
	legacyWrites   engine.LegacyWriteGate
}

func NewV1(gates ...engine.LegacyWriteGate) *ControllerV1 {
	var gate engine.LegacyWriteGate
	if len(gates) > 0 {
		gate = gates[0]
	}
	return &ControllerV1{legacyWrites: gate}
}

func requireBusinessWrite(ctx context.Context) error {
	return policy.Authorize(ctx, policy.PermissionBusinessWrite, policy.Resource{})
}

func requireAdmin(ctx context.Context) error {
	return policy.Authorize(ctx, policy.PermissionManageUsersPolicyGates, policy.Resource{})
}

// ---- 响应剧本 ----

func (c *ControllerV1) ListPlaybooks(ctx context.Context, _ *soarv1.ListPlaybooksReq) (*soarv1.ListPlaybooksRes, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	list, err := dao.ListPlaybooks(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]soarv1.PlaybookItem, 0, len(list))
	for _, p := range list {
		items = append(items, soarv1.PlaybookItem{
			ID: p.ID, Name: p.Name, Description: p.Description,
			Enabled: p.Enabled, CreatedAt: p.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	return &soarv1.ListPlaybooksRes{Items: items}, nil
}

func (c *ControllerV1) GetPlaybook(ctx context.Context, req *soarv1.GetPlaybookReq) (*soarv1.GetPlaybookRes, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	p, err := dao.GetPlaybook(ctx, req.ID)
	if err != nil {
		return nil, gerror.New("策略不存在")
	}
	return &soarv1.GetPlaybookRes{Item: soarv1.PlaybookItem{ID: p.ID, Name: p.Name, Description: p.Description, Enabled: p.Enabled, CreatedAt: p.CreatedAt.Format("2006-01-02 15:04:05")}}, nil
}

func (c *ControllerV1) CreatePlaybook(ctx context.Context, req *soarv1.CreatePlaybookReq) (*soarv1.CreatePlaybookRes, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	p := &dao.OpsPlaybook{Name: req.Name, Description: req.Description, Enabled: req.Enabled}
	if err := dao.CreatePlaybook(ctx, p); err != nil {
		return nil, err
	}
	return &soarv1.CreatePlaybookRes{ID: p.ID}, nil
}

func (c *ControllerV1) UpdatePlaybook(ctx context.Context, req *soarv1.UpdatePlaybookReq) (*soarv1.UpdatePlaybookRes, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	p, err := dao.GetPlaybook(ctx, req.ID)
	if err != nil {
		return nil, gerror.New("策略不存在")
	}
	if req.Name != "" {
		p.Name = req.Name
	}
	if req.Description != "" {
		p.Description = req.Description
	}
	if req.Enabled != nil {
		p.Enabled = *req.Enabled
	}
	return &soarv1.UpdatePlaybookRes{}, dao.UpdatePlaybook(ctx, p)
}

func (c *ControllerV1) DeletePlaybook(ctx context.Context, req *soarv1.DeletePlaybookReq) (*soarv1.DeletePlaybookRes, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	return &soarv1.DeletePlaybookRes{}, dao.DeletePlaybook(ctx, req.ID)
}

func (c *ControllerV1) ListRuns(ctx context.Context, req *soarv1.ListRunsReq) (*soarv1.ListRunsRes, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	list, err := dao.ListRuns(ctx, limit)
	if err != nil {
		return nil, err
	}
	items := make([]soarv1.RunItem, 0, len(list))
	for _, r := range list {
		items = append(items, toRunItem(r, nil))
	}
	return &soarv1.ListRunsRes{Items: items}, nil
}

func (c *ControllerV1) GetRun(ctx context.Context, req *soarv1.GetRunReq) (*soarv1.GetRunRes, error) {
	r, err := dao.GetRun(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	steps, _ := dao.GetRunSteps(ctx, req.ID)
	return &soarv1.GetRunRes{Item: toRunItem(*r, steps)}, nil
}

func (c *ControllerV1) GetStats(ctx context.Context, _ *soarv1.GetStatsReq) (*soarv1.GetStatsRes, error) {
	s, err := dao.GetOpsStats(ctx)
	if err != nil {
		return nil, err
	}
	return &soarv1.GetStatsRes{
		TotalRuns: s.TotalRuns, SuccessRuns: s.SuccessRuns, FailedRuns: s.FailedRuns,
	}, nil
}

func (c *ControllerV1) ClearRuns(ctx context.Context, _ *soarv1.ClearRunsReq) (*soarv1.ClearRunsRes, error) {
	if err := requireBusinessWrite(ctx); err != nil {
		return nil, err
	}
	return &soarv1.ClearRunsRes{}, dao.ClearRuns(ctx)
}

func (c *ControllerV1) DeleteRun(ctx context.Context, req *soarv1.DeleteRunReq) (*soarv1.DeleteRunRes, error) {
	if err := requireBusinessWrite(ctx); err != nil {
		return nil, err
	}
	return &soarv1.DeleteRunRes{}, dao.DeleteRun(ctx, req.ID)
}

func (c *ControllerV1) DirectRunForEvent(ctx context.Context, req *soarv1.DirectRunForEventReq) (*soarv1.DirectRunForEventRes, error) {
	if err := requireBusinessWrite(ctx); err != nil {
		return nil, err
	}
	event, err := dao.GetEventByID(ctx, req.EventID)
	if err != nil {
		return nil, gerror.New("事件不存在")
	}
	runID, err := c.directRunForEvent(ctx, event)
	if err != nil {
		return nil, err
	}
	return &soarv1.DirectRunForEventRes{RunID: runID}, nil
}

func (c *ControllerV1) TestPlaybook(ctx context.Context, req *soarv1.TestPlaybookReq) (*soarv1.TestPlaybookRes, error) {
	if err := requireBusinessWrite(ctx); err != nil {
		return nil, err
	}
	if _, err := dao.GetPlaybook(ctx, req.ID); err != nil {
		return nil, gerror.New("策略不存在")
	}
	event, err := dao.GetEventByID(ctx, req.EventID)
	if err != nil {
		return nil, gerror.New("事件不存在")
	}
	runID, err := c.directRunForEvent(ctx, event)
	if err != nil {
		return nil, err
	}
	return &soarv1.TestPlaybookRes{RunID: runID}, nil
}

func (c *ControllerV1) TriggerForEvent(ctx context.Context, req *soarv1.TriggerForEventReq) (*soarv1.TriggerForEventRes, error) {
	if err := requireBusinessWrite(ctx); err != nil {
		return nil, err
	}
	event, err := dao.GetEventByID(ctx, req.EventID)
	if err != nil {
		return nil, gerror.New("事件不存在")
	}
	runID, err := engine.TriggerForEvent(ctx, c.legacyWrites, event)
	if err != nil {
		return nil, err
	}
	return &soarv1.TriggerForEventRes{RunID: runID}, nil
}

func (c *ControllerV1) ListProtectedAssets(ctx context.Context, _ *soarv1.ListProtectedAssetsReq) (*soarv1.ListProtectedAssetsRes, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	list, err := dao.ListProtectedAssets(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]soarv1.ProtectedAssetItem, 0, len(list))
	for _, a := range list {
		items = append(items, soarv1.ProtectedAssetItem{ID: a.ID, AssetType: a.AssetType, Value: a.Value, Reason: a.Reason, CreatedAt: a.CreatedAt.Format("2006-01-02 15:04:05")})
	}
	return &soarv1.ListProtectedAssetsRes{Items: items}, nil
}

func (c *ControllerV1) CreateProtectedAsset(ctx context.Context, req *soarv1.CreateProtectedAssetReq) (*soarv1.CreateProtectedAssetRes, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	a := &dao.OpsProtectedAsset{AssetType: req.AssetType, Value: req.Value, Reason: req.Reason}
	if err := dao.CreateProtectedAsset(ctx, a); err != nil {
		return nil, err
	}
	return &soarv1.CreateProtectedAssetRes{ID: a.ID}, nil
}

func (c *ControllerV1) DeleteProtectedAsset(ctx context.Context, req *soarv1.DeleteProtectedAssetReq) (*soarv1.DeleteProtectedAssetRes, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	return &soarv1.DeleteProtectedAssetRes{}, dao.DeleteProtectedAsset(ctx, req.ID)
}

func (c *ControllerV1) directRunForEvent(ctx context.Context, event *dao.Event) (string, error) {
	if c == nil {
		return engine.DirectRunForEvent(ctx, nil, event)
	}
	return engine.DirectRunForEvent(ctx, c.legacyWrites, event)
}

func toRunItem(r dao.OpsRun, steps []dao.OpsRunStep) soarv1.RunItem {
	item := soarv1.RunItem{
		ID: r.ID, PlaybookID: r.PlaybookID, EventID: r.EventID,
		EventTitle: r.EventTitle, EventSeverity: r.EventSeverity, PlanSummary: r.PlanSummary,
		Status: r.Status, ErrorMsg: r.ErrorMsg, DurationMs: r.DurationMs,
		StartedAt: r.StartedAt.Format("2006-01-02 15:04:05"),
	}
	if r.FinishedAt != nil {
		item.FinishedAt = r.FinishedAt.Format("2006-01-02 15:04:05")
	}
	for _, s := range steps {
		item.Steps = append(item.Steps, soarv1.RunStepItem{
			ID: s.ID, StepOrder: s.StepOrder, ActionType: s.ActionType,
			Status: s.Status, Output: s.Output, ErrorMsg: s.ErrorMsg,
			RetryCount: s.RetryCount, DurationMs: s.DurationMs,
			StartedAt: s.StartedAt.Format("2006-01-02 15:04:05"),
		})
	}
	return item
}
