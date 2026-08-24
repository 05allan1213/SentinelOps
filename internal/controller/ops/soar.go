// Package ops AI 智能运维控制器
package ops

import (
	"context"

	soarv1 "SentinelOps/api/ops/v1"
	"SentinelOps/internal/ai/ops/engine"
	dao "SentinelOps/internal/dao/mysql"

	"github.com/gogf/gf/v2/errors/gerror"
)

type ControllerV1 struct{}

func NewV1() *ControllerV1 { return &ControllerV1{} }

// ---- Playbook ----

func (c *ControllerV1) ListPlaybooks(ctx context.Context, _ *soarv1.ListPlaybooksReq) (*soarv1.ListPlaybooksRes, error) {
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

func (c *ControllerV1) CreatePlaybook(ctx context.Context, req *soarv1.CreatePlaybookReq) (*soarv1.CreatePlaybookRes, error) {
	p := &dao.OpsPlaybook{Name: req.Name, Description: req.Description, Enabled: req.Enabled}
	if err := dao.CreatePlaybook(ctx, p); err != nil {
		return nil, err
	}
	return &soarv1.CreatePlaybookRes{ID: p.ID}, nil
}

func (c *ControllerV1) UpdatePlaybook(ctx context.Context, req *soarv1.UpdatePlaybookReq) (*soarv1.UpdatePlaybookRes, error) {
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
	return &soarv1.ClearRunsRes{}, dao.ClearRuns(ctx)
}

func (c *ControllerV1) DeleteRun(ctx context.Context, req *soarv1.DeleteRunReq) (*soarv1.DeleteRunRes, error) {
	return &soarv1.DeleteRunRes{}, dao.DeleteRun(ctx, req.ID)
}

func (c *ControllerV1) DirectRunForEvent(ctx context.Context, req *soarv1.DirectRunForEventReq) (*soarv1.DirectRunForEventRes, error) {
	event, err := dao.GetEventByID(ctx, req.EventID)
	if err != nil {
		return nil, gerror.New("事件不存在")
	}
	runID := engine.DirectRunForEvent(ctx, event)
	return &soarv1.DirectRunForEventRes{RunID: runID}, nil
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
