// Package trace 提供链路追踪查询 HTTP 控制器。
package trace

import (
	"context"
	"encoding/json"
	"fmt"

	v1 "SentinelOps/api/trace/v1"
	"SentinelOps/internal/service/trace"

	"github.com/gogf/gf/v2/frame/g"
)

// ControllerV1 链路追踪控制器
type ControllerV1 struct {
	service *trace.Service
}

// NewV1 创建控制器实例
func NewV1() *ControllerV1 {
	return &ControllerV1{
		service: trace.NewService(),
	}
}

// List 分页查询链路运行记录
func (c *ControllerV1) List(ctx context.Context, req *v1.ListReq) (*v1.ListRes, error) {
	list, total, err := c.service.ListRuns(ctx, req.Status, req.TraceId, req.SessionId, req.Page, req.PageSize)
	if err != nil {
		return &v1.ListRes{Total: 0, List: []v1.TraceRunVO{}}, nil
	}
	return &v1.ListRes{Total: total, List: list}, nil
}

// Detail 查询单条链路详情
func (c *ControllerV1) Detail(ctx context.Context, req *v1.DetailReq) (*v1.DetailRes, error) {
	runVO, nodeVOs, err := c.service.GetDetail(ctx, req.TraceId)
	if err != nil {
		return nil, err
	}
	return &v1.DetailRes{
		TraceRunVO: *runVO,
		Nodes:      nodeVOs,
	}, nil
}

// Stats 聚合查询统计数据
func (c *ControllerV1) Stats(ctx context.Context, req *v1.StatsReq) (*v1.StatsRes, error) {
	return c.service.GetStats(ctx, req.Days)
}

// BatchDelete 批量删除链路记录
func (c *ControllerV1) BatchDelete(ctx context.Context, req *v1.BatchDeleteReq) (*v1.BatchDeleteRes, error) {
	deleted, err := c.service.BatchDelete(ctx, req.TraceIds)
	if err != nil {
		return nil, err
	}
	return &v1.BatchDeleteRes{Deleted: deleted}, nil
}

// Export 导出单条链路为 JSON 文件
func (c *ControllerV1) Export(ctx context.Context, req *v1.ExportReq) (*v1.ExportRes, error) {
	runVO, nodeVOs, err := c.service.GetDetail(ctx, req.TraceId)
	if err != nil {
		return nil, err
	}

	type exportPayload struct {
		Run   v1.TraceRunVO    `json:"run"`
		Nodes []v1.TraceNodeVO `json:"nodes"`
	}
	payload := exportPayload{
		Run:   *runVO,
		Nodes: nodeVOs,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal export payload: %w", err)
	}

	r := g.RequestFromCtx(ctx)
	r.Response.ClearBuffer()
	r.Response.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="trace-%s.json"`, req.TraceId))
	r.Response.Header().Set("Content-Type", "application/json; charset=utf-8")
	r.Response.Write(data)
	r.Exit()
	return nil, nil
}

// SessionTimeline 查询指定会话的所有链路
func (c *ControllerV1) SessionTimeline(ctx context.Context, req *v1.SessionTimelineReq) (*v1.SessionTimelineRes, error) {
	return c.service.GetSessionTimeline(ctx, req.SessionId)
}

// CostOverview 成本概览
func (c *ControllerV1) CostOverview(ctx context.Context, req *v1.CostOverviewReq) (*v1.CostOverviewRes, error) {
	return c.service.GetCostOverview(ctx, req.StartDate, req.EndDate, req.Days)
}

// TokenTrend 实时 Token 消耗趋势
func (c *ControllerV1) TokenTrend(ctx context.Context, req *v1.TokenTrendReq) (*v1.TokenTrendRes, error) {
	return c.service.GetTokenTrend(ctx, req.Hours)
}

// ExportSessionSnapshot 导出会话对话快照（实时从 Redis 读取）
func (c *ControllerV1) ExportSessionSnapshot(ctx context.Context, req *v1.ExportSessionSnapshotReq) (*v1.ExportSessionSnapshotRes, error) {
	snapshot, err := c.service.GetSessionSnapshot(ctx, req.SessionId)
	if err != nil {
		g.Log().Warningf(ctx, "[ExportSessionSnapshot] 获取会话快照失败 | sessionId=%s | err=%v", req.SessionId, err)
		return nil, err
	}

	var data interface{}
	if err := json.Unmarshal([]byte(snapshot), &data); err == nil {
		if formatted, err := json.MarshalIndent(data, "", "  "); err == nil {
			snapshot = string(formatted)
		}
	}

	r := g.RequestFromCtx(ctx)
	r.Response.ClearBuffer()
	r.Response.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="session-%s-snapshot.json"`, req.SessionId))
	r.Response.Header().Set("Content-Type", "application/json; charset=utf-8")
	r.Response.Write([]byte(snapshot))
	r.Exit()

	g.Log().Infof(ctx, "[ExportSessionSnapshot] 会话对话快照导出成功 | sessionId=%s | size=%d bytes", req.SessionId, len(snapshot))
	return nil, nil
}
