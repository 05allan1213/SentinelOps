package effects

import (
	"errors"
	"strings"
	"testing"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/testutil/faultmatrix"
)

// TestFaultInjectionRepresentative 验证外部调用边界、Effect DAG 去重和
// unknown 结果的 fail-closed 标签，避免把不确定窗口伪装成成功。
func TestFaultInjectionRepresentative(t *testing.T) {
	matrix, err := faultmatrix.Load("../../../manifest/ci/fault-matrix.yaml")
	if err != nil {
		t.Fatalf("load P41 fault matrix: %v", err)
	}
	if err := matrix.Validate(); err != nil {
		t.Fatalf("validate P41 fault matrix: %v", err)
	}

	unknown := classifyInvocation("", errors.New("provider result unavailable"))
	if unknown.Class != InvocationUnknown || unknown.Retryable {
		t.Fatalf("unknown invocation=%+v, want non-retryable unknown", unknown)
	}
	if got := faultmatrix.OutcomeForInvocation(string(unknown.Class)); got != "PARKED AS DESIGNED" {
		t.Fatalf("unknown outcome=%q", got)
	}

	safeNotSent := classifyInvocation("", NewInvocationError(InvocationSafeNotSent, true, nil, errors.New("connection refused before send")))
	if safeNotSent.Class != InvocationSafeNotSent || !safeNotSent.Retryable {
		t.Fatalf("safe-not-sent invocation=%+v", safeNotSent)
	}
	if got := faultmatrix.OutcomeForInvocation(string(safeNotSent.Class)); got != "REPLAY PASS" {
		t.Fatalf("safe-not-sent outcome=%q", got)
	}

	entry, err := policy.LookupCatalog("block_ip")
	if err != nil {
		t.Fatalf("lookup block_ip catalog entry: %v", err)
	}
	dag, err := buildDAG("p41-run", strings.Repeat("a", 64), entry)
	if err != nil {
		t.Fatalf("build effect DAG: %v", err)
	}
	if len(dag) < 2 || dag[0].Key == dag[1].Key || dag[1].ParentKey != dag[0].Key {
		t.Fatalf("DAG identity/parent=%+v", dag)
	}
	if matrixCase, ok := matrix.Case("effect_succeeded_tool_return_crash"); !ok || matrixCase.ExpectedOutcome != "RESUME PASS" {
		t.Fatalf("effect reuse matrix case=%+v ok=%t", matrixCase, ok)
	}
}
