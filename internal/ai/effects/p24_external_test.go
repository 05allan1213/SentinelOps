package effects

import (
	"context"
	"errors"
	"testing"
	"time"

	"SentinelOps/internal/ai/policy"
)

func TestDerivedEffectDAGIdentityIsStableAndCollisionFree(t *testing.T) {
	entry, err := policy.LookupCatalog("block_ip")
	if err != nil {
		t.Fatal(err)
	}
	first, err := buildDAG("run-p24", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", entry)
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildDAG("run-p24", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", entry)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || len(second) != 2 || first[0] != second[0] || first[1] != second[1] {
		t.Fatalf("unstable DAG: first=%#v second=%#v", first, second)
	}
	if first[0].Role != "primary" || first[0].Step != "primary" || first[0].ParentKey != "" {
		t.Fatalf("primary=%#v", first[0])
	}
	if first[1].Role != "derived" || first[1].Step != "nginx_reload" || first[1].ParentKey != first[0].Key || first[1].Key == first[0].Key {
		t.Fatalf("derived=%#v primary=%#v", first[1], first[0])
	}
}

func TestEffectDeadlineLeavesLeaseSafetyMargin(t *testing.T) {
	now := time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC)
	leaseUntil := now.Add(time.Minute)
	deadline, err := boundedExternalDeadline(now, now.Add(2*time.Minute), leaseUntil, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !deadline.Equal(leaseUntil.Add(-10*time.Second)) || !deadline.Before(leaseUntil) {
		t.Fatalf("deadline=%s lease_until=%s", deadline, leaseUntil)
	}
	if _, err := boundedExternalDeadline(now, now.Add(time.Minute), now.Add(5*time.Second), 10*time.Second); !errors.Is(err, ErrExternalDeadlineUnsafe) {
		t.Fatalf("unsafe deadline error=%v", err)
	}
}

func TestIdempotencyKeyReachesProviderEndpoint(t *testing.T) {
	metadata := ExecutionMetadata{
		EffectKey: "effect-key-p24", EffectStep: "primary", EffectRole: "primary",
		EffectType: policy.EffectProviderIdempotent,
	}
	ctx := withExecutionMetadata(context.Background(), metadata)
	got, err := ExecutionMetadataFromContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != metadata {
		t.Fatalf("metadata=%#v want=%#v", got, metadata)
	}
}

func TestExternalEffectErrorClassificationIsFailClosed(t *testing.T) {
	safe := NewInvocationError(InvocationSafeNotSent, true, map[string]any{"phase": "dial"}, errors.New("dial rejected"))
	classified := classifyInvocation("", safe)
	if classified.Class != InvocationSafeNotSent || !classified.Retryable {
		t.Fatalf("safe classification=%#v", classified)
	}
	unknown := classifyInvocation("", errors.New("plain endpoint error"))
	if unknown.Class != InvocationUnknown || unknown.Retryable {
		t.Fatalf("plain error must fail closed as unknown: %#v", unknown)
	}
	definite := classifyInvocation("", NewInvocationError(InvocationDefiniteFailure, false, nil, errors.New("HTTP 400")))
	if definite.Class != InvocationDefiniteFailure || definite.Retryable {
		t.Fatalf("definite classification=%#v", definite)
	}
}
