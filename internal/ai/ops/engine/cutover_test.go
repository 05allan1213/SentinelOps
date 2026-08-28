package engine

import (
	"context"
	"errors"
	"testing"
)

type fixture26OpenLegacyGate struct{}

func (fixture26OpenLegacyGate) AllowLegacyOpsWrites(context.Context) bool { return true }

func TestNoDirectWriteLegacyGateIsExplicitAndClosedByDefault(t *testing.T) {
	if legacyOpsWritesAllowed(context.Background(), nil) {
		t.Fatal("nil legacy compatibility Gate opened direct writes")
	}
	if !legacyOpsWritesAllowed(context.Background(), fixture26OpenLegacyGate{}) {
		t.Fatal("explicit test Gate did not expose the retained legacy compatibility path")
	}
	if !errors.Is(requireLegacyOpsWrites(context.Background(), nil), ErrLegacyOpsWritesDisabled) {
		t.Fatalf("closed Gate error=%v, want ErrLegacyOpsWritesDisabled", requireLegacyOpsWrites(context.Background(), nil))
	}
}
