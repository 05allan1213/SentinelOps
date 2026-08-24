package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"SentinelOps/internal/dao/mysql"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestBaseBudgetPreCallHardStopAndSettlement(t *testing.T) {
	db := newP07Database(t, "p14_budget_hard_stop")
	store, ctx, token := p14BudgetRun(t, db, "hard-stop", BaseBudgetLimits{
		MaxModelCalls: 1, MaxL0ToolCalls: 1, MaxDurationMS: int64(time.Hour / time.Millisecond),
	}, time.Now().Add(time.Hour))
	model := p14ModelBudgetMetadata("provider_a/chat", "provider_a", "same-model")

	reservation, err := store.ReserveBaseBudget(ctx, ReserveBaseBudgetInput{
		Lease: token, Identity: "model-1", Kind: BaseBudgetKindModelCall, Subject: model.CatalogRef,
		TraceID: "trace-model-1", Metadata: model,
	})
	if err != nil {
		t.Fatalf("reserve first model call: %v", err)
	}
	if reservation.State != BaseBudgetReservationPending {
		t.Fatalf("reservation state = %q", reservation.State)
	}
	if err = store.SettleBaseBudget(ctx, SettleBaseBudgetInput{
		Lease: token, Identity: reservation.Identity, Outcome: BaseBudgetOutcomeSucceeded, TraceID: "trace-model-1",
	}); err != nil {
		t.Fatalf("settle model call: %v", err)
	}

	if _, err = store.ReserveBaseBudget(ctx, ReserveBaseBudgetInput{
		Lease: token, Identity: "model-2", Kind: BaseBudgetKindModelCall, Subject: model.CatalogRef,
		TraceID: "trace-model-2", Metadata: model,
	}); !errors.Is(err, ErrBaseBudgetExhausted) {
		t.Fatalf("second model reserve error = %v, want ErrBaseBudgetExhausted", err)
	}
	if _, err = store.ReserveBaseBudget(ctx, ReserveBaseBudgetInput{
		Lease: token, Identity: "tool-1", Kind: BaseBudgetKindL0ToolCall, Subject: "query_events",
		TraceID: "trace-tool-1", Metadata: BaseBudgetMetadata{ToolName: "query_events"},
	}); err != nil {
		t.Fatalf("reserve first L0 tool: %v", err)
	}
	if _, err = store.ReserveBaseBudget(ctx, ReserveBaseBudgetInput{
		Lease: token, Identity: "tool-2", Kind: BaseBudgetKindL0ToolCall, Subject: "query_events",
		TraceID: "trace-tool-2", Metadata: BaseBudgetMetadata{ToolName: "query_events"},
	}); !errors.Is(err, ErrBaseBudgetExhausted) {
		t.Fatalf("second tool reserve error = %v, want ErrBaseBudgetExhausted", err)
	}

	usage, reservations := p14ReadBudgetTruth(t, db, token.RunID)
	if usage.ModelCalls != 1 || usage.L0ToolCalls != 1 {
		t.Fatalf("usage = %+v", usage)
	}
	if reservations.Items["model-1"].State != BaseBudgetReservationSettled ||
		reservations.Items["model-2"].State != BaseBudgetReservationExhausted ||
		reservations.Items["tool-2"].State != BaseBudgetReservationExhausted {
		t.Fatalf("reservations = %+v", reservations.Items)
	}
	var reservedEvents, settledEvents, exhaustedEvents int64
	db.Model(&mysql.WorkflowEvent{}).Where("run_id = ? AND event_type = ?", token.RunID, EventBudgetReserved).Count(&reservedEvents)
	db.Model(&mysql.WorkflowEvent{}).Where("run_id = ? AND event_type = ?", token.RunID, EventBudgetSettled).Count(&settledEvents)
	db.Model(&mysql.WorkflowEvent{}).Where("run_id = ? AND event_type = ?", token.RunID, EventBudgetExhausted).Count(&exhaustedEvents)
	if reservedEvents != 2 || settledEvents != 1 || exhaustedEvents != 2 {
		t.Fatalf("budget events = reserved %d settled %d exhausted %d", reservedEvents, settledEvents, exhaustedEvents)
	}
}

func TestBudgetCrashReservationPreservedAcrossLeaseHandoff(t *testing.T) {
	db := newP07Database(t, "p14_budget_crash")
	store, ctx, firstLease := p14BudgetRun(t, db, "crash", BaseBudgetLimits{
		MaxModelCalls: 1, MaxL0ToolCalls: 1, MaxDurationMS: int64(time.Hour / time.Millisecond),
	}, time.Now().Add(time.Hour))
	request := ReserveBaseBudgetInput{
		Lease: firstLease, Identity: "stable-physical-call", Kind: BaseBudgetKindModelCall,
		Subject: "provider_a/chat", TraceID: "trace-before-crash",
		Metadata: p14ModelBudgetMetadata("provider_a/chat", "provider_a", "vendor-shared"),
	}
	first, err := store.ReserveBaseBudget(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	expiredRetry := request
	expiredRetry.Deadline = time.Now().Add(-time.Second)
	if _, err = store.ReserveBaseBudget(ctx, expiredRetry); !errors.Is(err, ErrBaseBudgetDeadlineExceeded) {
		t.Fatalf("pending reservation bypassed attempt deadline: %v", err)
	}
	// 模拟 reserve 与 endpoint 之间或 endpoint 与 settle 之间硬崩溃：不调用 settle。
	p09ExpireLease(t, db, firstLease.RunID)
	claimed, ok, err := store.ClaimNextRun(context.Background(), ClaimInput{Owner: "worker-after-crash", LeaseDuration: time.Minute})
	if err != nil || !ok || claimed == nil {
		t.Fatalf("claim after crash = %#v ok=%v err=%v", claimed, ok, err)
	}
	request.Lease = claimed.Token
	request.TraceID = "trace-after-crash"
	second, err := store.ReserveBaseBudget(ctx, request)
	if err != nil {
		t.Fatalf("idempotent reserve after lease handoff: %v", err)
	}
	if second.Identity != first.Identity || second.ReservedGeneration != first.ReservedGeneration || second.State != BaseBudgetReservationPending {
		t.Fatalf("reservation changed across crash: first=%+v second=%+v", first, second)
	}
	if _, err = store.ReserveBaseBudget(ctx, ReserveBaseBudgetInput{
		Lease: claimed.Token, Identity: "amplified-call", Kind: BaseBudgetKindModelCall,
		Subject: request.Subject, TraceID: "trace-amplified", Metadata: request.Metadata,
	}); !errors.Is(err, ErrBaseBudgetExhausted) {
		t.Fatalf("crash reset quota: %v", err)
	}
	usage, _ := p14ReadBudgetTruth(t, db, firstLease.RunID)
	if usage.ModelCalls != 1 {
		t.Fatalf("model usage after crash = %d, want 1", usage.ModelCalls)
	}
	if err = store.SettleBaseBudget(ctx, SettleBaseBudgetInput{
		Lease: claimed.Token, Identity: first.Identity, Outcome: BaseBudgetOutcomeSucceeded, TraceID: "trace-after-crash",
	}); err != nil {
		t.Fatalf("settle original reservation from new generation: %v", err)
	}
}

func TestBaseBudgetDeadlineDurationAndReservationIdentity(t *testing.T) {
	db := newP07Database(t, "p14_budget_time")
	store, ctx, deadlineLease := p14BudgetRun(t, db, "deadline", BaseBudgetLimits{
		MaxModelCalls: 2, MaxL0ToolCalls: 2, MaxDurationMS: int64(time.Hour / time.Millisecond),
	}, time.Now().Add(-time.Second))
	if _, err := store.ReserveBaseBudget(ctx, ReserveBaseBudgetInput{
		Lease: deadlineLease, Identity: "past-deadline", Kind: BaseBudgetKindL0ToolCall, Subject: "query_events",
		TraceID: "trace-deadline", Metadata: BaseBudgetMetadata{ToolName: "query_events"},
	}); !errors.Is(err, ErrBaseBudgetDeadlineExceeded) {
		t.Fatalf("deadline error = %v", err)
	}
	attemptDeadlineStore, attemptDeadlineCtx, attemptDeadlineLease := p14BudgetRun(t, db, "attempt_deadline", BaseBudgetLimits{
		MaxModelCalls: 2, MaxL0ToolCalls: 2, MaxDurationMS: int64(time.Hour / time.Millisecond),
	}, time.Now().Add(time.Hour))
	if _, err := attemptDeadlineStore.ReserveBaseBudget(attemptDeadlineCtx, ReserveBaseBudgetInput{
		Lease: attemptDeadlineLease, Identity: "past-attempt-deadline", Kind: BaseBudgetKindL0ToolCall, Subject: "query_events",
		TraceID: "trace-attempt-deadline", Deadline: time.Now().Add(-time.Second), Metadata: BaseBudgetMetadata{ToolName: "query_events"},
	}); !errors.Is(err, ErrBaseBudgetDeadlineExceeded) {
		t.Fatalf("attempt deadline error = %v", err)
	}

	durationStore, durationCtx, durationLease := p14BudgetRun(t, db, "duration", BaseBudgetLimits{
		MaxModelCalls: 2, MaxL0ToolCalls: 2, MaxDurationMS: 1,
	}, time.Now().Add(time.Hour))
	if err := db.Model(&mysql.WorkflowRun{}).Where("id = ?", durationLease.RunID).
		Update("started_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := durationStore.ReserveBaseBudget(durationCtx, ReserveBaseBudgetInput{
		Lease: durationLease, Identity: "duration-exceeded", Kind: BaseBudgetKindL0ToolCall, Subject: "query_events",
		TraceID: "trace-duration", Metadata: BaseBudgetMetadata{ToolName: "query_events"},
	}); !errors.Is(err, ErrBaseBudgetDeadlineExceeded) {
		t.Fatalf("duration error = %v", err)
	}

	identityStore, identityCtx, identityLease := p14BudgetRun(t, db, "identity", BaseBudgetLimits{
		MaxModelCalls: 2, MaxL0ToolCalls: 1, MaxDurationMS: int64(time.Hour / time.Millisecond),
	}, time.Now().Add(time.Hour))
	request := ReserveBaseBudgetInput{
		Lease: identityLease, Identity: "identity-one", Kind: BaseBudgetKindModelCall,
		Subject: "provider_a/shared", TraceID: "trace-identity-a",
		Metadata: p14ModelBudgetMetadata("provider_a/shared", "provider_a", "same-vendor-model"),
	}
	if _, err := identityStore.ReserveBaseBudget(identityCtx, request); err != nil {
		t.Fatal(err)
	}
	tampered := request
	tampered.Subject = "provider_b/shared"
	tampered.Metadata = p14ModelBudgetMetadata("provider_b/shared", "provider_b", "same-vendor-model")
	if _, err := identityStore.ReserveBaseBudget(identityCtx, tampered); !errors.Is(err, ErrBudgetReservationConflict) {
		t.Fatalf("reservation identity metadata conflict = %v", err)
	}
	request.Identity = "identity-two"
	request.Subject = "provider_b/shared"
	request.Metadata = tampered.Metadata
	if _, err := identityStore.ReserveBaseBudget(identityCtx, request); err != nil {
		t.Fatalf("same vendor model across Provider collided: %v", err)
	}
}

func TestBaseBudgetConcurrentCAS(t *testing.T) {
	db := newP07Database(t, "p14_budget_concurrent")
	db.Logger = logger.Default.LogMode(logger.Silent)
	store, ctx, token := p14BudgetRun(t, db, "concurrent", BaseBudgetLimits{
		MaxModelCalls: 16, MaxL0ToolCalls: 1, MaxDurationMS: int64(time.Hour / time.Millisecond),
	}, time.Now().Add(time.Hour))
	const callers = 128
	start := make(chan struct{})
	results := make(chan error, callers)
	var wait sync.WaitGroup
	for index := 0; index < callers; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := store.ReserveBaseBudget(ctx, ReserveBaseBudgetInput{
				Lease: token, Identity: fmt.Sprintf("concurrent-%03d", index), Kind: BaseBudgetKindModelCall,
				Subject: "provider_a/chat", TraceID: fmt.Sprintf("trace-%03d", index),
				Metadata: p14ModelBudgetMetadata("provider_a/chat", "provider_a", "vendor-chat"),
			})
			results <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	successes, exhausted := 0, 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrBaseBudgetExhausted):
			exhausted++
		default:
			t.Fatalf("concurrent reserve: %v", err)
		}
	}
	if successes != 16 || exhausted != callers-16 {
		t.Fatalf("concurrent results = success %d exhausted %d", successes, exhausted)
	}
	usage, _ := p14ReadBudgetTruth(t, db, token.RunID)
	if usage.ModelCalls != 16 {
		t.Fatalf("concurrent durable usage = %d, want 16", usage.ModelCalls)
	}
}

func TestBaseBudgetRejectsTamperedDurableTruth(t *testing.T) {
	db := newP07Database(t, "p14_budget_tamper")
	store, ctx, token := p14BudgetRun(t, db, "tamper", BaseBudgetLimits{
		MaxModelCalls: 2, MaxL0ToolCalls: 2, MaxDurationMS: int64(time.Hour / time.Millisecond),
	}, time.Now().Add(time.Hour))
	request := ReserveBaseBudgetInput{
		Lease: token, Identity: "tamper-first", Kind: BaseBudgetKindModelCall,
		Subject: "provider_a/chat", TraceID: "trace-tamper-first",
		Metadata: p14ModelBudgetMetadata("provider_a/chat", "provider_a", "vendor-chat"),
	}
	if _, err := store.ReserveBaseBudget(ctx, request); err != nil {
		t.Fatal(err)
	}
	tamperedUsage := `{"schema":"sentinelops/run-base-budget/v1","model_calls":0,"l0_tool_calls":0}`
	if err := db.Model(&mysql.WorkflowRun{}).Where("id = ?", token.RunID).
		Update("budget_usage_json", tamperedUsage).Error; err != nil {
		t.Fatal(err)
	}
	request.Identity = "tamper-second"
	request.TraceID = "trace-tamper-second"
	if _, err := store.ReserveBaseBudget(ctx, request); !errors.Is(err, ErrBaseBudgetLimitsInvalid) {
		t.Fatalf("tampered durable truth error = %v, want ErrBaseBudgetLimitsInvalid", err)
	}
}

func p14BudgetRun(t *testing.T, db *gorm.DB, suffix string, limits BaseBudgetLimits, deadline time.Time) (*GORMStore, context.Context, LeaseToken) {
	t.Helper()
	store := NewGORMStore(db)
	ctx := p08UserContext("user-p14-" + suffix)
	input := p08CreateInput("run-p14-"+suffix, "session-p14-"+suffix)
	limitsJSON, err := json.Marshal(limits)
	if err != nil {
		t.Fatal(err)
	}
	input.BudgetLimitsJSON = limitsJSON
	input.DeadlineAt = deadline.UTC().Truncate(time.Millisecond)
	if _, err = store.CreateRunWithSessionLock(ctx, input); err != nil {
		t.Fatalf("create P14 budget Run: %v", err)
	}
	claimed, ok, err := store.ClaimNextRun(context.Background(), ClaimInput{Owner: "worker-p14-" + suffix, LeaseDuration: time.Minute})
	if err != nil || !ok || claimed == nil || claimed.Token.RunID != input.ID {
		t.Fatalf("claim P14 budget Run = %#v ok=%v err=%v", claimed, ok, err)
	}
	return store, ctx, claimed.Token
}

func p14ReadBudgetTruth(t *testing.T, db *gorm.DB, runID string) (BaseBudgetUsage, BaseBudgetReservations) {
	t.Helper()
	var run mysql.WorkflowRun
	if err := db.First(&run, "id = ?", runID).Error; err != nil {
		t.Fatal(err)
	}
	var usage BaseBudgetUsage
	var reservations BaseBudgetReservations
	if run.BudgetUsageJSON == nil || json.Unmarshal([]byte(*run.BudgetUsageJSON), &usage) != nil {
		t.Fatalf("invalid budget usage: %v", run.BudgetUsageJSON)
	}
	if run.BudgetReservationsJSON == nil || json.Unmarshal([]byte(*run.BudgetReservationsJSON), &reservations) != nil {
		t.Fatalf("invalid budget reservations: %v", run.BudgetReservationsJSON)
	}
	return usage, reservations
}

func p14ModelBudgetMetadata(catalogRef, provider, modelID string) BaseBudgetMetadata {
	return BaseBudgetMetadata{
		CatalogRef: catalogRef, Provider: provider, Driver: "openai_compatible_chat", ModelID: modelID,
		Profile: "default", SnapshotIdentity: catalogRef + "\x00" + provider + "\x00openai_compatible_chat\x00" + modelID,
	}
}
