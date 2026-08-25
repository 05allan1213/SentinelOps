package tools_test

import (
	"context"
	"errors"
	"testing"

	"SentinelOps/internal/ai/effects"
	"SentinelOps/internal/ai/policy"
	toolsintelligence "SentinelOps/internal/ai/tools/intelligence"
	toolsops "SentinelOps/internal/ai/tools/ops"
	toolsreport "SentinelOps/internal/ai/tools/report"

	"github.com/cloudwego/eino/components/tool"
)

func TestNoDirectWriteEveryMutationEndpointRejectsUngatedLegacyCall(t *testing.T) {
	ctx := policy.WithIdentity(context.Background(), policy.Identity{
		UserID: "operator-p26", Role: policy.RoleOperator, Scope: policy.Scope{UserID: "operator-p26"},
	})
	cases := []struct {
		name string
		tool tool.InvokableTool
		args string
	}{
		{name: "create_report", tool: toolsreport.NewCreateReportTool(), args: `{"title":"p26","content":"p26"}`},
		{name: "save_intelligence", tool: toolsintelligence.NewSaveIntelligenceTool(), args: `{"title":"p26","content":"p26"}`},
		{name: "update_event_status", tool: toolsops.NewUpdateEventStatusTool(), args: `{"event_id":"p26","status":"resolved"}`},
		{name: "block_ip", tool: toolsops.NewBlockIPTool(), args: `{"ip":"192.0.2.26"}`},
		{name: "notify_dingtalk", tool: toolsops.NewNotifyDingTalkTool(), args: `{"title":"p26","content":"p26"}`},
		{name: "notify_wecom", tool: toolsops.NewNotifyWeComTool(), args: `{"content":"p26"}`},
		{name: "notify_email", tool: toolsops.NewNotifyEmailTool(), args: `{"subject":"p26"}`},
		{name: "webhook_out", tool: toolsops.NewWebhookOutTool(), args: `{"url":"https://example.invalid","payload":"{}"}`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.tool.InvokableRun(ctx, test.args); !errors.Is(err, effects.ErrMutationRouteRequired) {
				t.Fatalf("direct endpoint error=%v, want ErrMutationRouteRequired", err)
			}
		})
	}
}
