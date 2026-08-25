package trace

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"SentinelOps/internal/ai/policy"

	langfuse "github.com/cloudwego/eino-ext/callbacks/langfuse/v2"
	"github.com/cloudwego/eino/callbacks"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// LangfuseOptions 只保存官方 Handler 所需的非持久化启动参数。
type LangfuseOptions struct {
	StaticEnabled           bool
	DynamicEnabled          bool
	Host                    string
	PublicKey               string
	SecretKey               string
	ServiceName             string
	Environment             string
	Release                 string
	Version                 string
	SampleRate              float64
	Timeout                 time.Duration
	MaxAttributeValueLength int
	MaxSpanAttributeBytes   int
	HTTPClient              *http.Client
	SpanExporter            sdktrace.SpanExporter
}

// LangfuseRuntime 只持有官方 CallbackHandler，不实现 Callback bus、Exporter 或 Span 转换。
type LangfuseRuntime struct {
	handler *langfuse.CallbackHandler
}

var installedLangfuse struct {
	sync.RWMutex
	runtime *LangfuseRuntime
}

// NewLangfuseRuntime 仅在静态配置和动态 Gate 同时开启时构造官方 Handler。
func NewLangfuseRuntime(ctx context.Context, options LangfuseOptions) (*LangfuseRuntime, error) {
	if !options.StaticEnabled || !options.DynamicEnabled {
		return nil, nil
	}
	if ctx == nil {
		return nil, fmt.Errorf("Langfuse startup context is required")
	}
	if options.SpanExporter == nil && (options.Host == "" || options.PublicKey == "" || options.SecretKey == "") {
		return nil, fmt.Errorf("enabled Langfuse requires host, public key and secret key")
	}
	redactor := policy.NewRedactor()
	handler, err := langfuse.NewHandler(ctx, &langfuse.Config{
		Host: options.Host, PublicKey: options.PublicKey, SecretKey: options.SecretKey,
		ServiceName: options.ServiceName, Environment: options.Environment,
		Release: options.Release, Version: options.Version, SampleRate: options.SampleRate,
		Timeout:                 options.Timeout,
		MaxAttributeValueLength: options.MaxAttributeValueLength,
		MaxSpanAttributeBytes:   options.MaxSpanAttributeBytes,
		MaskFunc:                redactor.RedactText,
		HTTPClient:              options.HTTPClient,
		SpanExporter:            options.SpanExporter,
	})
	if err != nil {
		return nil, fmt.Errorf("create official Langfuse handler: %w", err)
	}
	return &LangfuseRuntime{handler: handler}, nil
}

// Handler 返回应直接注册到 Eino Global Callback 链的官方 Handler。
func (r *LangfuseRuntime) Handler() callbacks.Handler {
	if r == nil {
		return nil
	}
	return r.handler
}

// StartAttempt 为一个 durable Attempt 创建 Langfuse 根观察；MySQL trace_id 只作为关联 metadata。
func (r *LangfuseRuntime) StartAttempt(ctx context.Context, metadata AttemptMetadata, userID string) context.Context {
	if r == nil || r.handler == nil || ctx == nil {
		return ctx
	}
	models := make([]map[string]any, 0, len(metadata.Models))
	for _, model := range metadata.Models {
		if model.valid() {
			models = append(models, model.metadata())
		}
	}
	return r.handler.StartTrace(ctx,
		langfuse.WithName("durable.attempt"),
		langfuse.WithObservationType(langfuse.ObservationTypeAgent),
		langfuse.WithInput(metadata.Query),
		langfuse.WithUserID(userID),
		langfuse.WithSessionID(metadata.SessionID),
		langfuse.WithVersion(metadata.RuntimeVersion),
		langfuse.WithTags("runtime:durable_v1"),
		langfuse.WithMetadataValues(map[string]any{
			"run_id": metadata.RunID, "attempt": metadata.Attempt,
			"lease_generation": metadata.LeaseGeneration,
			"mysql_trace_id":   metadata.TraceID,
			"runtime_version":  metadata.RuntimeVersion,
			"model_snapshots":  models,
			"usage_semantics": map[string]any{
				"input_includes_cached":     true,
				"output_includes_reasoning": true,
				"cached_input_separate":     true,
				"reasoning_separate":        true,
			},
		}),
	)
}

// EndAttempt 调用官方 EndTrace；重复调用由官方 Handler 保证幂等。
func (r *LangfuseRuntime) EndAttempt(ctx context.Context, output string) {
	if r != nil && r.handler != nil {
		r.handler.EndTrace(ctx, policy.NewRedactor().RedactText(output))
	}
}

// Flush 调用官方 Flush，不实现自定义 drain。
func (r *LangfuseRuntime) Flush(ctx context.Context) error {
	if r == nil || r.handler == nil {
		return nil
	}
	return r.handler.Flush(ctx)
}

// Shutdown 调用官方 Shutdown，不实现自定义 Span Processor 生命周期。
func (r *LangfuseRuntime) Shutdown(ctx context.Context) error {
	if r == nil || r.handler == nil {
		return nil
	}
	return r.handler.Shutdown(ctx)
}

// InstallLangfuseRuntime 保存 bootstrap 构造的唯一可选官方 Handler。
func InstallLangfuseRuntime(runtime *LangfuseRuntime) {
	installedLangfuse.Lock()
	defer installedLangfuse.Unlock()
	installedLangfuse.runtime = runtime
}

func currentLangfuseRuntime() *LangfuseRuntime {
	installedLangfuse.RLock()
	defer installedLangfuse.RUnlock()
	return installedLangfuse.runtime
}

// ShutdownInstalledLangfuse 在 bootstrap deadline 内 Flush 并 Shutdown 唯一官方 Handler。
func ShutdownInstalledLangfuse(ctx context.Context) error {
	installedLangfuse.Lock()
	runtime := installedLangfuse.runtime
	installedLangfuse.runtime = nil
	installedLangfuse.Unlock()
	if runtime == nil {
		return nil
	}
	if err := runtime.Flush(ctx); err != nil {
		_ = runtime.Shutdown(ctx)
		return err
	}
	return runtime.Shutdown(ctx)
}
