package workflow

import "testing"

func TestTraceIncompleteEventPrecedesRunCompleted(t *testing.T) {
	events, err := completionTraceEvents(CompleteRunInput{TargetStatus: RunStatusSucceeded, TraceQuality: TraceQualityIncomplete, TraceID: "trace-p35"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Type != EventTraceIncomplete || events[1].Type != EventRunCompleted {
		t.Fatalf("completion events = %#v", events)
	}
}

func TestTraceFlushedEventPrecedesRunCompleted(t *testing.T) {
	events, err := completionTraceEvents(CompleteRunInput{TargetStatus: RunStatusSucceeded, TraceQuality: TraceQualityComplete, TraceID: "trace-p35"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Type != EventTraceFlushed || events[1].Type != EventRunCompleted {
		t.Fatalf("completion events = %#v", events)
	}
}

func TestTraceFlushedEventRequiresAttemptTraceID(t *testing.T) {
	events, err := completionTraceEvents(CompleteRunInput{TargetStatus: RunStatusSucceeded, TraceQuality: TraceQualityComplete})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != EventRunCompleted {
		t.Fatalf("legacy completion events = %#v", events)
	}
}
