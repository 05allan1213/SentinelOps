package chat

import (
	"context"
	"errors"
	"reflect"
	"testing"

	v1 "SentinelOps/api/chat/v1"
	"SentinelOps/internal/ai/runtime"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
	chatsvc "SentinelOps/internal/service/chat"
)

// TestFileUploadTraceEntryPointMatchesRegisteredRoute 固定 Trace 入口与真实路由一致：
// 路由路径的唯一来源是 FileUploadReq 的 g.Meta path，旧值曾指向不存在的
// /api/chat/v1/file_upload，并会原样显示在 Traces 详情页。
func TestFileUploadTraceEntryPointMatchesRegisteredRoute(t *testing.T) {
	meta, ok := reflect.TypeOf(v1.FileUploadReq{}).FieldByName("Meta")
	if !ok {
		t.Fatal("FileUploadReq 缺少 g.Meta 路由声明")
	}
	route := "/api" + meta.Tag.Get("path")
	if meta.Tag.Get("path") == "" {
		t.Fatal("FileUploadReq g.Meta 未声明 path")
	}
	if fileUploadEntryPoint != route {
		t.Fatalf("trace entry point = %q, want registered route %q", fileUploadEntryPoint, route)
	}
}

func TestSSEReadUsesRequestCancellation(t *testing.T) {
	var observed error
	service, err := chatsvc.NewDurableService(chatsvc.DurableServiceConfig{
		AcceptNewRuns: true,
		Snapshot:      runtime.FrozenRuntimeSnapshot{},
		CreateRun:     func(context.Context, workflow.CreateRunInput) (*mysql.WorkflowRun, error) { return nil, nil },
		ListEvents: func(ctx context.Context, _ string, _ int64) ([]workflow.StreamEvent, error) {
			observed = ctx.Err()
			return nil, ctx.Err()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = streamDurableEvents(ctx, service, "run-canceled-sse", 0, func(workflow.StreamEvent) {})
	if !errors.Is(err, context.Canceled) || !errors.Is(observed, context.Canceled) {
		t.Fatalf("stream error=%v observed read context=%v, want request cancellation", err, observed)
	}
}
