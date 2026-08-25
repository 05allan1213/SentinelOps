package runtime

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"SentinelOps/internal/ai/models"
	appconfig "SentinelOps/internal/config"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestRetryPhysicalCallsUseDistinctReservationsAndCatalogRefs(t *testing.T) {
	input := p14SnapshotInput(t)
	input.Models = []ModelSnapshot{
		p27ModelSnapshot("provider_a/shared", "provider_a", 0),
		p27ModelSnapshot("provider_b/shared", "provider_b", 1),
	}
	frozen, err := FreezeRuntimeSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	budget := newP14RecordingBudget()
	ctx, _ := p14InvocationContextWithSnapshot(t, "run-p27-physical", "user-p27", 1, budget, frozen)
	p27EnablePhysicalCalls(t, ctx)

	var observed []CallMetadata
	endpoint := func() model.BaseChatModel {
		return &p14Model{validate: func(callCtx context.Context) error {
			metadata, metadataErr := CallMetadataFromContext(callCtx)
			if metadataErr == nil {
				observed = append(observed, metadata)
			}
			return metadataErr
		}}
	}
	binder := PhysicalModelBinder(NewRuntimeHandler())
	first, err := binder(p27CandidateIdentity("provider_a/shared", "provider_a", 0), endpoint())
	if err != nil {
		t.Fatal(err)
	}
	second, err := binder(p27CandidateIdentity("provider_b/shared", "provider_b", 1), endpoint())
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []model.BaseChatModel{first, first, second} {
		if _, err = candidate.Generate(ctx, []*schema.Message{schema.UserMessage("inspect")}); err != nil {
			t.Fatal(err)
		}
	}
	if len(observed) != 3 || observed[0].ReservationIdentity == observed[1].ReservationIdentity ||
		observed[1].ReservationIdentity == observed[2].ReservationIdentity {
		t.Fatalf("physical call metadata did not receive distinct reservations: %#v", observed)
	}
	if observed[0].Model.CatalogRef != "provider_a/shared" || observed[2].Model.CatalogRef != "provider_b/shared" ||
		observed[0].Model.ModelID != observed[2].Model.ModelID {
		t.Fatalf("provider-qualified metadata collapsed or changed vendor Model ID: %#v", observed)
	}
	if reserved, settled := budget.counts(); reserved != 3 || settled != 3 {
		t.Fatalf("budget counts = %d/%d, want 3/3", reserved, settled)
	}
}

func TestFailoverCandidateMustMatchFrozenSnapshot(t *testing.T) {
	input := p14SnapshotInput(t)
	input.Models = []ModelSnapshot{p27ModelSnapshot("provider_a/shared", "provider_a", 0)}
	frozen, err := FreezeRuntimeSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	budget := newP14RecordingBudget()
	ctx, _ := p14InvocationContextWithSnapshot(t, "run-p27-frozen", "user-p27", 1, budget, frozen)
	p27EnablePhysicalCalls(t, ctx)
	bound, err := PhysicalModelBinder(NewRuntimeHandler())(
		p27CandidateIdentity("provider_b/shared", "provider_b", 0), &p14Model{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = bound.Generate(ctx, nil); err == nil || !strings.Contains(err.Error(), "Frozen Runtime Snapshot") {
		t.Fatalf("frozen candidate mismatch error = %v", err)
	}
	if reserved, settled := budget.counts(); reserved != 0 || settled != 0 {
		t.Fatalf("mismatched candidate reached budget = %d/%d", reserved, settled)
	}
}

func TestStreamFailureSettlesPhysicalCallAsFailed(t *testing.T) {
	input := p14SnapshotInput(t)
	input.Models = []ModelSnapshot{p27ModelSnapshot("provider_a/shared", "provider_a", 0)}
	frozen, err := FreezeRuntimeSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	budget := newP14RecordingBudget()
	ctx, _ := p14InvocationContextWithSnapshot(t, "run-p27-stream", "user-p27", 1, budget, frozen)
	p27EnablePhysicalCalls(t, ctx)
	bound, err := PhysicalModelBinder(NewRuntimeHandler())(
		p27CandidateIdentity("provider_a/shared", "provider_a", 0), &p27FailingStreamModel{},
	)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := bound.Stream(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = stream.Recv(); err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("stream read error = %v, want endpoint failure", err)
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if len(budget.reservations) != 1 || len(budget.settled) != 1 {
		t.Fatalf("stream budget counts = %d/%d, want 1/1", len(budget.reservations), len(budget.settled))
	}
	for _, settlement := range budget.settled {
		if settlement.Succeeded {
			t.Fatal("failed stream was settled as succeeded")
		}
	}
}

func TestNoDoubleRetryLeavesEinoFailoverProxyUnwrapped(t *testing.T) {
	proxy := &p27FailoverProxyModel{}
	wrapped, err := NewRuntimeHandler().WrapModel(context.Background(), proxy, &adk.ModelContext{})
	if err != nil {
		t.Fatal(err)
	}
	if wrapped != proxy {
		t.Fatal("Eino failover proxy was wrapped at the logical call layer")
	}
}

func p27ModelSnapshot(catalogRef, provider string, order int) ModelSnapshot {
	return ModelSnapshot{
		Kind: "chat", Profile: "default", CandidateOrder: order, CatalogRef: catalogRef,
		Provider: provider, Driver: appconfig.DriverOpenAICompatibleChat, ModelID: "shared-vendor-id",
		Pricing: PricingSnapshot{Revision: provider + "-v1", Currency: "CNY", Unit: "per_million_tokens", Input: 1, Output: 2},
	}
}

func p27CandidateIdentity(catalogRef, provider string, order int) models.CandidateIdentity {
	return models.CandidateIdentity{
		CatalogRef: catalogRef, Provider: provider, Driver: appconfig.DriverOpenAICompatibleChat,
		ModelID: "shared-vendor-id", Profile: "default", Order: order,
	}
}

func p27EnablePhysicalCalls(t *testing.T, ctx context.Context) {
	t.Helper()
	attempt, err := AttemptContextFromContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	attempt.physicalCalls = &atomic.Uint64{}
}

type p27FailingStreamModel struct{}

func (*p27FailingStreamModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return nil, errors.New("unexpected Generate")
}

func (*p27FailingStreamModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	reader, writer := schema.Pipe[*schema.Message](1)
	go func() {
		defer writer.Close()
		writer.Send(nil, errors.New("stream interrupted"))
	}()
	return reader, nil
}

type p27FailoverProxyModel struct{ p14Model }

func (*p27FailoverProxyModel) GetType() string { return "FailoverProxyModel" }
