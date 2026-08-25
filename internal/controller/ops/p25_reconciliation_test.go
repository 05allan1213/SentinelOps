package ops

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"SentinelOps/internal/ai/workflow"
)

func TestReconciliationAPIOnlySubmitsDecisions(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate reconciliation controller source")
	}
	source, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "reconciliation.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"internal/ai/effects", "internal/ai/tools", ".Execute("} {
		if strings.Contains(string(source), forbidden) {
			t.Fatalf("reconciliation API contains forbidden execution path %q", forbidden)
		}
	}
}

func TestReconciliationResolutionValidationIsClosed(t *testing.T) {
	for _, resolution := range []string{workflow.EffectResolutionExecuted, workflow.EffectResolutionNotExecuted, workflow.EffectResolutionStillUnknown} {
		if !validReconciliationResolution(resolution) {
			t.Fatalf("valid resolution rejected: %q", resolution)
		}
	}
	for _, resolution := range []string{"", "succeeded", "accepted_unknown", "pending"} {
		if validReconciliationResolution(resolution) {
			t.Fatalf("unknown resolution accepted: %q", resolution)
		}
	}
}
