package event

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	v1 "SentinelOps/api/event/v1"
	"SentinelOps/internal/ai/agent/event_analysis_pipeline"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

type legacyCompatibilityGateFunc func(context.Context) bool

func (f legacyCompatibilityGateFunc) AllowLegacyCompatibility(ctx context.Context) bool {
	return f(ctx)
}

type recordingStream struct {
	events []string
	done   int
}

func (s *recordingStream) Send(event, data string) { s.events = append(s.events, event+":"+data) }
func (s *recordingStream) Done()                   { s.done++ }

// assertStreamEnvelope 断言数据帧是前端可解析的 JSON envelope（type/content），
// 而不是会被 JSON.parse 失败分支静默丢弃的纯文本。
func assertStreamEnvelope(t *testing.T, frame, wantType string) map[string]string {
	t.Helper()
	eventName, data, ok := strings.Cut(frame, ":")
	if !ok {
		t.Fatalf("stream frame %q is missing an event name", frame)
	}
	if eventName != wantType {
		t.Fatalf("stream event name = %q, want %q", eventName, wantType)
	}
	payload := map[string]string{}
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		t.Fatalf("stream data %q is not JSON envelope: %v", data, err)
	}
	if payload["type"] != wantType {
		t.Fatalf("stream envelope type = %q, want %q", payload["type"], wantType)
	}
	return payload
}

func TestPipelineStreamClosedGateSkipsLegacyAgentInitialization(t *testing.T) {
	stream := new(recordingStream)
	c := NewV1(legacyCompatibilityGateFunc(func(context.Context) bool { return false }))
	c.newStream = func(context.Context) streamClient { return stream }
	calls := 0
	c.loadAgent = func(context.Context) (compose.Runnable[*event_analysis_pipeline.UserMessage, *schema.Message], error) {
		calls++
		return nil, nil
	}

	if _, err := c.PipelineStream(context.Background(), &v1.PipelineStreamReq{Query: "blocked"}); err != nil {
		t.Fatal(err)
	}
	if calls != 0 || stream.done != 1 || len(stream.events) != 1 {
		t.Fatalf("agent_calls=%d stream=%#v", calls, stream)
	}
	payload := assertStreamEnvelope(t, stream.events[0], "error")
	if payload["content"] != legacyCompatibilityDisabledMessage {
		t.Fatalf("stream error content = %q, want %q", payload["content"], legacyCompatibilityDisabledMessage)
	}
}

func TestAnalyzeSingleStreamClosedGateSkipsModelInitialization(t *testing.T) {
	stream := new(recordingStream)
	c := NewV1()
	c.newStream = func(context.Context) streamClient { return stream }
	calls := 0
	c.loadModel = func(context.Context) (model.ToolCallingChatModel, error) {
		calls++
		return nil, nil
	}

	if _, err := c.AnalyzeSingleStream(context.Background(), &v1.AnalyzeSingleStreamReq{}); err != nil {
		t.Fatal(err)
	}
	if calls != 0 || stream.done != 1 || len(stream.events) != 1 {
		t.Fatalf("model_calls=%d stream=%#v", calls, stream)
	}
	payload := assertStreamEnvelope(t, stream.events[0], "error")
	if payload["content"] != legacyCompatibilityDisabledMessage {
		t.Fatalf("stream error content = %q, want %q", payload["content"], legacyCompatibilityDisabledMessage)
	}
}
