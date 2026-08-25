package bootstrap

import (
	"os"
	"strings"
	"testing"
)

func TestMutationRouteWorkerBuildsTheDurablePlanWithTheHITLHandler(t *testing.T) {
	source, err := os.ReadFile("worker.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, required := range []string{"BuildDurableRuntimeSnapshot", "NewHITLRuntimeHandler", "NewDurablePlanAgent"} {
		if !strings.Contains(text, required) {
			t.Errorf("durable Worker production wiring missing %q", required)
		}
	}
	if strings.Contains(text, `name != "event_analysis_agent"`) {
		t.Fatal("durable Worker still rejects the Plan Agent and all Mutation specialists")
	}
}
