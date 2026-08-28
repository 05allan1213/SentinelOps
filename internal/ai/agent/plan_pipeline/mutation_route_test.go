package plan_pipeline

import (
	"os"
	"strings"
	"testing"
)

func TestMutationRoutePlanWorkersReceiveTheSharedRuntimeHandler(t *testing.T) {
	executorSource, err := os.ReadFile("executor.go")
	if err != nil {
		t.Fatal(err)
	}
	workerSource, err := os.ReadFile("agent_worker.go")
	if err != nil {
		t.Fatal(err)
	}
	for name, source := range map[string]string{"executor.go": string(executorSource), "agent_worker.go": string(workerSource)} {
		if !strings.Contains(source, "RuntimeHandler") {
			t.Errorf("%s does not carry the shared RuntimeHandler", name)
		}
	}
	for _, direct := range []string{"RunWithQuery", "ExecuteRun", "execAction"} {
		if strings.Contains(string(workerSource), direct) {
			t.Errorf("AgentTool Worker retained direct mutation call %q", direct)
		}
	}
}
