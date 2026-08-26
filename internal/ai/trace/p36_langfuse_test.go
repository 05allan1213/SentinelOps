package trace

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type p36CountingExporter struct {
	inner    *tracetest.InMemoryExporter
	shutdown atomic.Int32
}

type p36RoundTripFunc func(*http.Request) (*http.Response, error)

func (fn p36RoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func (e *p36CountingExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	return e.inner.ExportSpans(ctx, spans)
}

func (e *p36CountingExporter) Shutdown(ctx context.Context) error {
	e.shutdown.Add(1)
	return e.inner.Shutdown(ctx)
}

func TestLangfuseGateDisabledMakesZeroNetworkCalls(t *testing.T) {
	var calls atomic.Int32
	runtime, err := NewLangfuseRuntime(context.Background(), LangfuseOptions{
		StaticEnabled:  false,
		DynamicEnabled: true,
		Host:           "http://127.0.0.1:1",
		PublicKey:      "public-test",
		SecretKey:      "secret-test",
		HTTPClient: &http.Client{Transport: p36RoundTripFunc(func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			return nil, context.Canceled
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	if runtime != nil || calls.Load() != 0 {
		t.Fatalf("disabled Langfuse runtime=%v network_calls=%d, want nil/0", runtime, calls.Load())
	}
}

func TestLangfuseOfficialLifecycleMetadataUsageAndRedaction(t *testing.T) {
	const secret = "sk-ABCDEFGHIJKLMNOPQRSTUV"
	exporter := &p36CountingExporter{inner: tracetest.NewInMemoryExporter()}
	runtime, err := NewLangfuseRuntime(context.Background(), LangfuseOptions{
		StaticEnabled:           true,
		DynamicEnabled:          true,
		ServiceName:             "sentinelops-test",
		Environment:             "test",
		SampleRate:              1,
		MaxAttributeValueLength: 512,
		MaxSpanAttributeBytes:   4096,
		SpanExporter:            exporter,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := runtime.StartAttempt(context.Background(), AttemptMetadata{
		TraceID: "mysql-trace-p36", RunID: "run-p36", SessionID: "session-p36",
		Attempt: 2, LeaseGeneration: 7, RuntimeVersion: "runtime-v36",
		Query: "api_key=" + secret,
		Models: []ModelMetadata{{
			Kind: "chat", CatalogRef: "provider-a/chat", Provider: "provider-a", Driver: "openai_compatible_chat",
			ModelID: "shared-model", Profile: "reasoning", RouteOptions: map[string]any{"enable_thinking": true},
			SnapshotIdentity: strings.Repeat("a", 64), PricingRevision: "price-v1", PricingCurrency: "CNY",
			PricingUnit: "per_million_tokens", InputPrice: 1, CachedInputPrice: 0.5, OutputPrice: 2,
		}},
	}, "user-p36")
	info := &callbacks.RunInfo{Name: "SharedModel", Component: components.ComponentOfChatModel}
	generationCtx := runtime.handler.OnStart(ctx, info, &model.CallbackInput{
		Messages: []*schema.Message{schema.UserMessage("Authorization: Bearer " + secret)},
		Config:   &model.Config{Model: "shared-model"},
	})
	runtime.handler.OnEnd(generationCtx, info, &model.CallbackOutput{Message: &schema.Message{
		Role: schema.Assistant, Content: "token=" + secret,
		ResponseMeta: &schema.ResponseMeta{Usage: &schema.TokenUsage{
			PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30,
			PromptTokenDetails:      schema.PromptTokenDetails{CachedTokens: 2},
			CompletionTokensDetails: schema.CompletionTokensDetails{ReasoningTokens: 5},
		}},
	}})
	runtime.EndAttempt(ctx, "password="+secret)
	flushCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runtime.Flush(flushCtx); err != nil {
		t.Fatal(err)
	}
	spans := exporter.inner.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("exported spans=%d, want root and generation", len(spans))
	}
	encoded := ""
	for _, span := range spans {
		for _, attribute := range span.Attributes {
			encoded += string(attribute.Key) + "=" + attribute.Value.Emit() + "\n"
		}
	}
	for _, want := range []string{
		"langfuse.trace.metadata.run_id=run-p36",
		"langfuse.trace.metadata.mysql_trace_id=mysql-trace-p36",
		"langfuse.trace.metadata.attempt=2",
		"provider-a/chat",
		"gen_ai.usage.cache_read.input_tokens=2",
		"gen_ai.usage.reasoning.output_tokens=5",
	} {
		if !strings.Contains(encoded, want) {
			t.Fatalf("Langfuse attributes missing %q:\n%s", want, encoded)
		}
	}
	if strings.Contains(encoded, secret) || !strings.Contains(encoded, "[REDACTED]") {
		t.Fatalf("Langfuse attributes were not uniformly redacted:\n%s", encoded)
	}
	for _, span := range spans {
		for _, attribute := range span.Attributes {
			if len(attribute.Value.Emit()) > 512 {
				t.Fatalf("attribute %s exceeded configured limit", attribute.Key)
			}
		}
	}
	if err := runtime.Shutdown(flushCtx); err != nil {
		t.Fatal(err)
	}
	if exporter.shutdown.Load() != 1 {
		t.Fatalf("shutdown calls=%d, want 1", exporter.shutdown.Load())
	}
}

func TestLangfuseAttemptInitializationFailureShutsDownOwnedRuntime(t *testing.T) {
	exporter := &p36CountingExporter{inner: tracetest.NewInMemoryExporter()}
	runtime, err := NewLangfuseRuntime(context.Background(), LangfuseOptions{
		StaticEnabled: true, DynamicEnabled: true, SpanExporter: exporter,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, barrier, err := StartAttempt(context.Background(), AttemptMetadata{}, runtime); err == nil || barrier != nil {
		t.Fatalf("StartAttempt barrier=%v err=%v, want initialization failure", barrier, err)
	}
	if exporter.shutdown.Load() != 1 {
		t.Fatalf("shutdown calls=%d, want 1", exporter.shutdown.Load())
	}
}
