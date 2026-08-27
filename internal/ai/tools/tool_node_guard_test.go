package tools

import (
	"context"
	"strings"
	"testing"
)

func TestUnknownToolHandlerReturnsRecoverableMessage(t *testing.T) {
	handler := UnknownToolHandler([]string{"block_ip", "query_events"})
	got, err := handler(context.Background(), "search_event_by_id", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `tool "search_event_by_id" does not exist`) ||
		!strings.Contains(got, "block_ip") || !strings.Contains(got, "query_events") {
		t.Fatalf("unexpected unknown-tool reply: %s", got)
	}
}

func TestNormalizeTriggerOpsArgumentsObjectToJSONString(t *testing.T) {
	args := `{"event_id":"evt-1","proposals":[{"tool_name":"block_ip","arguments_json":{"ip":"192.0.2.10"}}]}`
	got, err := NormalizeTriggerOpsArguments(context.Background(), "trigger_ops", args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `"arguments_json":"{\"ip\":\"192.0.2.10\"}"`) {
		t.Fatalf("object arguments_json 未被规范化为字符串: %s", got)
	}
}

func TestNormalizeTriggerOpsArgumentsKeepsStringAndOtherTools(t *testing.T) {
	stringArgs := `{"event_id":"evt-1","proposals":[{"tool_name":"block_ip","arguments_json":"{\"ip\":\"192.0.2.10\"}"}]}`
	got, err := NormalizeTriggerOpsArguments(context.Background(), "trigger_ops", stringArgs)
	if err != nil || got != stringArgs {
		t.Fatalf("字符串形态被意外改写: got=%s err=%v", got, err)
	}
	other, err := NormalizeTriggerOpsArguments(context.Background(), "query_events", stringArgs)
	if err != nil || other != stringArgs {
		t.Fatalf("非 trigger_ops 工具被意外改写: got=%s err=%v", other, err)
	}
}
