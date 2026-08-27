package ops

import (
	"context"
	"errors"
	"testing"

	"SentinelOps/internal/ai/ops/engine"
	dao "SentinelOps/internal/dao/mysql"
)

type recordingLegacyWriteGate struct {
	allow  bool
	called int
}

func (g *recordingLegacyWriteGate) AllowLegacyOpsWrites(context.Context) bool {
	g.called++
	return g.allow
}

func TestControllerPassesLegacyWriteGateAndDefaultsClosed(t *testing.T) {
	if _, err := NewV1().directRunForEvent(context.Background(), &dao.Event{}); !errors.Is(err, engine.ErrLegacyOpsWritesDisabled) {
		t.Fatalf("default Gate error=%v, want ErrLegacyOpsWritesDisabled", err)
	}

	gate := &recordingLegacyWriteGate{}
	if _, err := NewV1(gate).directRunForEvent(context.Background(), &dao.Event{}); !errors.Is(err, engine.ErrLegacyOpsWritesDisabled) {
		t.Fatalf("closed Gate error=%v, want ErrLegacyOpsWritesDisabled", err)
	}
	if gate.called != 1 {
		t.Fatalf("legacy Gate calls=%d, want 1", gate.called)
	}

	gate.allow = true
	if runID, err := NewV1(gate).directRunForEvent(context.Background(), &dao.Event{}); err != nil || runID != "" {
		t.Fatalf("open compatibility Gate run_id=%q err=%v", runID, err)
	}
}
