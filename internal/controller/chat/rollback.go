package chat

import (
	"context"

	v1 "SentinelOps/api/chat/v1"
	chatsvc "SentinelOps/internal/service/chat"

	"github.com/gogf/gf/v2/frame/g"
)

// Rollback 对话回溯
func (c *ControllerV1) Rollback(ctx context.Context, req *v1.RollbackReq) (*v1.RollbackRes, error) {
	g.Log().Infof(ctx, "[Rollback] 收到回溯请求 | session=%s | targetIndex=%d", req.SessionId, req.TargetIndex)

	removedCount, err := chatsvc.RollbackSession(ctx, req.SessionId, req.TargetIndex)
	if err != nil {
		g.Log().Errorf(ctx, "[Rollback] 回溯失败 | session=%s | err=%v", req.SessionId, err)
		return nil, err
	}

	g.Log().Infof(ctx, "[Rollback] 回溯成功 | session=%s | targetIndex=%d | removedCount=%d",
		req.SessionId, req.TargetIndex, removedCount)

	return &v1.RollbackRes{
		Success:      true,
		RolledBackTo: req.TargetIndex,
		RemovedCount: removedCount,
	}, nil
}
