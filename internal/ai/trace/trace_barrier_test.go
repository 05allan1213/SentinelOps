package trace

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTraceAttemptIdentityAndNodeCorrelation(t *testing.T) {
	metadata := AttemptMetadata{
		TraceID: "trace-attempt-2", RunID: "run-shared", Attempt: 2,
		LeaseGeneration: 7, RuntimeVersion: "runtime-v1",
	}
	active, err := newActiveTrace(context.Background(), metadata)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := active.nodeMetadata(ModelMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"run-shared", "trace-attempt-2", "runtime-v1", `"attempt":2`, `"lease_generation":7`} {
		if !strings.Contains(encoded, want) {
			t.Fatalf("node metadata %s does not contain %q", encoded, want)
		}
	}
	if next := metadata; next.TraceID == active.TraceID {
		next.TraceID = "trace-attempt-3"
		if next.TraceID == active.TraceID {
			t.Fatal("a new Attempt reused the previous trace_id")
		}
	}
}

func TestTraceAttemptModelSnapshotCostAndUsage(t *testing.T) {
	model := ModelMetadata{
		Kind: "chat", CatalogRef: "provider-a/shared", Provider: "provider-a", Driver: "openai-compatible-chat",
		ModelID: "same-vendor-id", Profile: "default", SnapshotIdentity: strings.Repeat("a", 64),
		PricingRevision: "price-a", PricingCurrency: "CNY", PricingUnit: "per_million_tokens",
		InputPrice: 10, CachedInputPrice: 2, OutputPrice: 30,
	}
	if got := model.cost(1_000_000, 250_000, 500_000, 100_000); got != 23 {
		t.Fatalf("snapshot cost = %v, want 23", got)
	}
	metadata, err := json.Marshal(model.metadata())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"provider-a/shared", "provider-a", "openai-compatible-chat", "same-vendor-id", "price-a"} {
		if !strings.Contains(string(metadata), want) {
			t.Fatalf("model metadata %s does not contain %q", metadata, want)
		}
	}
}

func TestTraceAttemptUsesNodeKindForSharedVendorModelID(t *testing.T) {
	models := []ModelMetadata{
		{Kind: "chat", CatalogRef: "provider-a/chat", Provider: "provider-a", Driver: "chat", ModelID: "shared", Profile: "default", SnapshotIdentity: strings.Repeat("a", 64), PricingRevision: "chat-v1", InputPrice: 1},
		{Kind: "embedding", CatalogRef: "provider-a/embedding", Provider: "provider-a", Driver: "embedding", ModelID: "shared", Profile: "default", SnapshotIdentity: strings.Repeat("b", 64), PricingRevision: "embedding-v1", InputPrice: 2},
		{Kind: "rerank", CatalogRef: "provider-a/rerank", Provider: "provider-a", Driver: "rerank", ModelID: "shared", Profile: "default", SnapshotIdentity: strings.Repeat("c", 64), PricingRevision: "rerank-v1", InputPrice: 3},
	}
	active, err := newActiveTrace(context.Background(), AttemptMetadata{
		TraceID: "trace-kind", RunID: "run-kind", Attempt: 1, LeaseGeneration: 1,
		RuntimeVersion: "runtime-v1", Models: models,
	})
	if err != nil {
		t.Fatal(err)
	}
	active.SetNodeType("rerank-node", NodeTypeRerank)
	kind := modelKindForNodeType(active.GetNodeType("rerank-node"))
	got := active.resolveModel(kind, "shared")
	if got.Kind != "rerank" || got.PricingRevision != "rerank-v1" {
		t.Fatalf("resolved model = kind %q pricing %q, want rerank snapshot", got.Kind, got.PricingRevision)
	}
}

func TestTraceAttemptNeverFallsBackToModelNamePricing(t *testing.T) {
	active, err := newActiveTrace(context.Background(), AttemptMetadata{
		TraceID: "trace-pricing", RunID: "run-pricing", Attempt: 1,
		LeaseGeneration: 1, RuntimeVersion: "runtime-v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := active.costForModel(context.Background(), ModelMetadata{}, "qwen-prefix-that-might-match", 1_000_000, 0, 0, 0); got != 0 {
		t.Fatalf("durable Attempt fallback cost = %v, want 0 without a valid frozen Snapshot", got)
	}

	snapshot := ModelMetadata{
		Kind: "rerank", CatalogRef: "provider-a/rerank", Provider: "provider-a", Driver: "rerank",
		ModelID: "qwen-prefix-that-might-match", Profile: "default", SnapshotIdentity: strings.Repeat("d", 64),
		PricingRevision: "snapshot-price", InputPrice: 7,
	}
	if got := active.costForModel(context.Background(), snapshot, snapshot.ModelID, 1_000_000, 0, 0, 0); got != 7 {
		t.Fatalf("durable Attempt snapshot cost = %v, want 7", got)
	}
}

func TestTraceRedactionCoversPromptToolApprovalAndEffect(t *testing.T) {
	secret := "sk-ABCDEFGHIJKLMNOPQRSTUV"
	redacted, err := redactTraceValue(map[string]any{
		"prompt":   "Authorization: Bearer " + secret,
		"tool":     map[string]any{"api_key": secret},
		"approval": map[string]any{"secret": secret},
		"effect":   "smtp_password=" + secret,
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(redacted)
	if strings.Contains(string(encoded), secret) || !strings.Contains(string(encoded), "[REDACTED]") {
		t.Fatalf("trace payload was not uniformly redacted: %s", encoded)
	}
}

func TestTraceFlushWaitsForTrackedWrites(t *testing.T) {
	release := make(chan struct{})
	barrier := newTestBarrier(func(context.Context) error {
		<-release
		return nil
	})
	done := make(chan string, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		done <- barrier.Flush(ctx)
	}()
	select {
	case <-done:
		t.Fatal("Flush returned before the tracked write completed")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if quality := <-done; quality != TraceQualityComplete {
		t.Fatalf("quality = %q, want %q", quality, TraceQualityComplete)
	}
}

func TestTraceIncompleteOnFlushTimeout(t *testing.T) {
	release := make(chan struct{})
	barrier := newTestBarrier(func(context.Context) error {
		<-release
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if quality := barrier.Flush(ctx); quality != TraceQualityIncomplete {
		t.Fatalf("quality = %q, want %q", quality, TraceQualityIncomplete)
	}
	close(release)
}
