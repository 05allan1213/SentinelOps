package chat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	apichat "SentinelOps/api/chat"
	v2 "SentinelOps/api/chat/v2"
	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
	chatsvc "SentinelOps/internal/service/chat"
	"SentinelOps/utility/sse"

	"github.com/gogf/gf/v2/frame/g"
)

const durableEventPollInterval = 250 * time.Millisecond

// ControllerV2 只调用 durable Run 创建和 Event 读取服务。
type ControllerV2 struct {
	service *chatsvc.DurableService
}

// NewV2 创建不持有 Runner、Agent 或执行 goroutine 的 durable Controller。
func NewV2(service *chatsvc.DurableService) apichat.IChatV2 {
	return &ControllerV2{service: service}
}

// CreateRun 只写 pending Run；同 Session 冲突稳定映射为 409。
func (c *ControllerV2) CreateRun(ctx context.Context, req *v2.CreateRunReq) (*v2.CreateRunRes, error) {
	run, err := c.service.CreateRun(ctx, chatsvc.CreateDurableRunRequest{
		SessionID: req.SessionID, Query: req.Query, Agent: req.Agent,
	})
	if err != nil {
		request := g.RequestFromCtx(ctx)
		if status := durableHTTPStatus(err); status != 0 {
			request.Response.WriteHeader(status)
		}
		return nil, err
	}
	return &v2.CreateRunRes{RunID: run.ID, SessionID: run.SessionID, Status: run.Status}, nil
}

func durableHTTPStatus(err error) int {
	switch {
	case errors.Is(err, workflow.ErrSessionRunActive):
		return http.StatusConflict
	case errors.Is(err, chatsvc.ErrDurableReconnectInvalid):
		return http.StatusBadRequest
	case errors.Is(err, chatsvc.ErrDurableRunNotFound):
		return http.StatusNotFound
	case errors.Is(err, chatsvc.ErrDurableRunForbidden):
		return http.StatusForbidden
	case errors.Is(err, chatsvc.ErrDurableRunGateClosed):
		return http.StatusServiceUnavailable
	case errors.Is(err, policy.ErrForbidden):
		return http.StatusForbidden
	default:
		return 0
	}
}

// RunEvents 只 replay/tail workflow_events；断开只结束读取，不取消 Worker Run。
func (c *ControllerV2) RunEvents(ctx context.Context, req *v2.RunEventsReq) (*v2.RunEventsRes, error) {
	client := sse.NewClient(g.RequestFromCtx(ctx))
	err := streamDurableEvents(ctx, c.service, req.RunID, req.AfterSeq, func(event workflow.StreamEvent) {
		payload, marshalErr := json.Marshal(event.Payload)
		if marshalErr != nil {
			payload = []byte(`null`)
		}
		client.SendEvent(event.ID, event.Type, string(payload))
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		if status := durableHTTPStatus(err); status != 0 {
			g.RequestFromCtx(ctx).Response.WriteHeader(status)
		}
		return nil, err
	}
	client.Done()
	return nil, nil
}

func streamDurableEvents(ctx context.Context, service *chatsvc.DurableService, runID string, afterSeq int64, send func(workflow.StreamEvent)) error {
	cursor := afterSeq
	firstRead := true
	ticker := time.NewTicker(durableEventPollInterval)
	defer ticker.Stop()
	for {
		events, err := service.Events(ctx, runID, cursor)
		if err != nil {
			return err
		}
		if firstRead && len(events) == 0 && cursor > 0 {
			boundary, boundaryErr := service.Events(ctx, runID, cursor-1)
			if boundaryErr != nil {
				return boundaryErr
			}
			for _, event := range boundary {
				if event.ID == cursor && isDurableStreamStopEvent(event) {
					return nil
				}
			}
		}
		firstRead = false
		for _, event := range events {
			send(event)
			cursor = event.ID
			if isDurableStreamStopEvent(event) {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func isDurableStreamStopEvent(event workflow.StreamEvent) bool {
	switch event.Type {
	case workflow.EventRunCompleted, workflow.EventRunParked:
		return true
	case workflow.EventRunFailed:
		envelope, ok := event.Payload.(map[string]any)
		if !ok {
			return true
		}
		data, ok := envelope["data"].(map[string]any)
		if !ok {
			return true
		}
		retryable, _ := data["retryable"].(bool)
		return !retryable
	default:
		return false
	}
}
