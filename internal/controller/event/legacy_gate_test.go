package event

import (
	"context"
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
	if calls != 0 || stream.done != 1 || len(stream.events) != 1 || stream.events[0] != "error:"+legacyCompatibilityDisabledMessage {
		t.Fatalf("agent_calls=%d stream=%#v", calls, stream)
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
	if calls != 0 || stream.done != 1 || len(stream.events) != 1 || stream.events[0] != "error:"+legacyCompatibilityDisabledMessage {
		t.Fatalf("model_calls=%d stream=%#v", calls, stream)
	}
}
