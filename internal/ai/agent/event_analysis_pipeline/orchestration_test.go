package event_analysis_pipeline

import "testing"

func TestEventAnalysisMaxStepAllowsComplexDeepThinkingTasks(t *testing.T) {
	if eventAnalysisMaxStep < 25 {
		t.Fatalf("事件分析 Agent 的步数预算过小，got=%d want>=%d", eventAnalysisMaxStep, 25)
	}
}
