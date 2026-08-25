package models

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"SentinelOps/internal/ai/breaker"
	"SentinelOps/internal/ai/limiter"
	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
	appconfig "SentinelOps/internal/config"

	modelopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// CandidateIdentity 是物理候选的 provider-qualified 非敏感身份。
type CandidateIdentity struct {
	CatalogRef string
	Provider   string
	Driver     string
	ModelID    string
	Profile    string
	Order      int
}

// PhysicalModelBinder 把现有 RuntimeHandler 放到每次真实候选调用位置。
type PhysicalModelBinder func(CandidateIdentity, model.BaseChatModel) (model.BaseChatModel, error)

// Reliability 是所有 ChatModelAgent builder 复用的唯一官方可靠性配置。
type Reliability struct {
	initial  model.BaseChatModel
	retry    *adk.ModelRetryConfig
	failover *adk.ModelFailoverConfig[*schema.Message]
}

var processHealth = struct {
	sync.Mutex
	settings breaker.Settings
	registry *breaker.Registry
}{}

// BuildReliability 从当前配置构造候选，并只组合 Eino 官方 Retry/Failover。
func BuildReliability(ctx context.Context, profile string, binder PhysicalModelBinder) (*Reliability, error) {
	cfg, err := appconfig.Current()
	if err != nil {
		return nil, err
	}
	return buildReliability(ctx, cfg, profile, binder, buildOpenAIChatModel)
}

type candidateFactory func(context.Context, chatProfile) (model.BaseChatModel, error)

func buildReliability(ctx context.Context, cfg *appconfig.Config, profile string, binder PhysicalModelBinder, factory candidateFactory) (*Reliability, error) {
	if ctx == nil || cfg == nil || binder == nil || factory == nil {
		return nil, fmt.Errorf("reliability context, configuration, binder and factory are required")
	}
	resolved, err := resolveChatCandidates(cfg, profile)
	if err != nil {
		return nil, err
	}
	if err = limiter.Configure(limiter.Settings{QPS: cfg.ModelReliability.Limiter.QPS, Burst: cfg.ModelReliability.Limiter.Burst}); err != nil {
		return nil, err
	}
	breakerSettings := breaker.Settings{
		FailureThreshold: cfg.ModelReliability.Breaker.FailureThreshold,
		OpenTimeout:      time.Duration(cfg.ModelReliability.Breaker.OpenTimeoutMS) * time.Millisecond,
	}
	registry, err := processBreakerRegistry(breakerSettings)
	if err != nil {
		return nil, err
	}
	candidates := make([]model.BaseChatModel, 0, len(resolved))
	for index, candidate := range resolved {
		raw, createErr := factory(ctx, candidate)
		if createErr != nil {
			return nil, fmt.Errorf("build chat candidate %s: %w", candidate.CatalogRef, createErr)
		}
		identity := CandidateIdentity{
			CatalogRef: candidate.CatalogRef, Provider: candidate.Provider, Driver: candidate.Driver,
			ModelID: candidate.Model, Profile: profile, Order: index,
		}
		physical, bindErr := binder(identity, raw)
		if bindErr != nil {
			return nil, fmt.Errorf("bind chat candidate %s: %w", candidate.CatalogRef, bindErr)
		}
		health, healthErr := registry.For(candidate.CatalogRef)
		if healthErr != nil {
			return nil, healthErr
		}
		candidates = append(candidates, &candidateModel{endpoint: physical, health: health, order: index})
	}
	retry := &adk.ModelRetryConfig{
		MaxRetries:  cfg.ModelReliability.Retry.MaxRetries,
		IsRetryAble: func(_ context.Context, callErr error) bool { return isRetryable(callErr) },
		BackoffFunc: func(_ context.Context, attempt int) time.Duration {
			if attempt < 1 {
				attempt = 1
			}
			return time.Duration(cfg.ModelReliability.Retry.BaseBackoffMS) * time.Millisecond * time.Duration(1<<min(attempt-1, 6))
		},
	}
	failover := &adk.ModelFailoverConfig[*schema.Message]{
		MaxRetries: uint(len(candidates) - 1),
		ShouldFailover: func(_ context.Context, _ *schema.Message, callErr error) bool {
			return errors.Is(callErr, breaker.ErrOpen) || isRetryable(callErr)
		},
		GetFailoverModel: func(_ context.Context, failoverContext *adk.FailoverContext[*schema.Message]) (model.BaseChatModel, []*schema.Message, error) {
			if failoverContext == nil {
				return nil, nil, fmt.Errorf("failover context is required")
			}
			failedOrder, ok := failedCandidateOrder(failoverContext.LastErr)
			if !ok {
				return nil, nil, fmt.Errorf("failover attempt %d is missing failed candidate identity", failoverContext.FailoverAttempt)
			}
			index := (failedOrder + 1) % len(candidates)
			if index < 0 || index >= len(candidates) {
				return nil, nil, fmt.Errorf("failover candidate attempt %d is out of range", failoverContext.FailoverAttempt)
			}
			return candidates[index], nil, nil
		},
	}
	return &Reliability{initial: candidates[0], retry: retry, failover: failover}, nil
}

// ConfigureChatModelAgent 应用唯一可靠性入口，供现有及后续 ChatModelAgent builder 复用。
func ConfigureChatModelAgent(config *adk.ChatModelAgentConfig, reliability *Reliability) error {
	if config == nil || reliability == nil || reliability.initial == nil || reliability.retry == nil || reliability.failover == nil {
		return fmt.Errorf("complete ChatModelAgent reliability configuration is required")
	}
	if config.Model != nil || config.ModelRetryConfig != nil || config.ModelFailoverConfig != nil {
		return fmt.Errorf("ChatModelAgent reliability must be configured exactly once")
	}
	config.Model = reliability.initial
	config.ModelRetryConfig = reliability.retry
	config.ModelFailoverConfig = reliability.failover
	return nil
}

func processBreakerRegistry(settings breaker.Settings) (*breaker.Registry, error) {
	if settings.FailureThreshold <= 0 || settings.OpenTimeout <= 0 {
		return nil, fmt.Errorf("model breaker requires positive threshold and timeout")
	}
	processHealth.Lock()
	defer processHealth.Unlock()
	if processHealth.registry == nil {
		processHealth.settings = settings
		processHealth.registry = breaker.NewRegistry(settings)
	} else if processHealth.settings != settings {
		return nil, fmt.Errorf("model breaker settings changed after process registry initialization")
	}
	return processHealth.registry, nil
}

func buildOpenAIChatModel(ctx context.Context, candidate chatProfile) (model.BaseChatModel, error) {
	var created model.BaseChatModel
	err := appconfig.UseSecret(ctx, candidate.SecretRef, func(secret []byte) error {
		endpoint, createErr := modelopenai.NewChatModel(ctx, &modelopenai.ChatModelConfig{
			APIKey: string(secret), BaseURL: candidate.BaseURL, Model: candidate.Model, ExtraFields: candidate.ExtraFields,
		})
		created = endpoint
		return createErr
	})
	return created, err
}

type candidateModel struct {
	endpoint model.BaseChatModel
	health   *breaker.Health
	order    int
}

func (m *candidateModel) Generate(ctx context.Context, input []*schema.Message, options ...model.Option) (*schema.Message, error) {
	if err := m.health.Allow(); err != nil {
		return nil, m.callError(err)
	}
	result, err := m.endpoint.Generate(ctx, input, options...)
	m.record(err)
	return result, m.callError(err)
}

func (m *candidateModel) Stream(ctx context.Context, input []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	if err := m.health.Allow(); err != nil {
		return nil, m.callError(err)
	}
	stream, err := m.endpoint.Stream(ctx, input, options...)
	if err != nil {
		m.record(err)
		return nil, m.callError(err)
	}
	if stream == nil {
		err = fmt.Errorf("model candidate returned nil stream")
		m.record(err)
		return nil, m.callError(err)
	}
	reader, writer := schema.Pipe[*schema.Message](1)
	go func() {
		defer stream.Close()
		defer writer.Close()
		for {
			message, receiveErr := stream.Recv()
			if errors.Is(receiveErr, io.EOF) {
				m.health.Success()
				return
			}
			if receiveErr != nil {
				m.record(receiveErr)
				writer.Send(nil, m.callError(receiveErr))
				return
			}
			if writer.Send(message, nil) {
				return
			}
		}
	}()
	return reader, nil
}

type candidateCallError struct {
	order int
	err   error
}

func (e *candidateCallError) Error() string { return e.err.Error() }
func (e *candidateCallError) Unwrap() error { return e.err }

func (m *candidateModel) callError(err error) error {
	if err == nil {
		return nil
	}
	return &candidateCallError{order: m.order, err: err}
}

func failedCandidateOrder(err error) (int, bool) {
	var exhausted *adk.RetryExhaustedError
	if errors.As(err, &exhausted) {
		err = exhausted.LastErr
	}
	var candidateErr *candidateCallError
	if !errors.As(err, &candidateErr) {
		return 0, false
	}
	return candidateErr.order, true
}

func (m *candidateModel) record(err error) {
	if err == nil {
		m.health.Success()
		return
	}
	if isRetryable(err) {
		m.health.Failure()
	}
}

func isRetryable(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, adk.ErrStreamCanceled) || errors.Is(err, breaker.ErrOpen) ||
		errors.Is(err, policy.ErrUnauthenticated) || errors.Is(err, policy.ErrForbidden) ||
		errors.Is(err, policy.ErrMutationDisabled) || errors.Is(err, policy.ErrUnknownCatalogTool) ||
		errors.Is(err, workflow.ErrBaseBudgetExhausted) || errors.Is(err, workflow.ErrBaseBudgetDeadlineExceeded) ||
		errors.Is(err, workflow.ErrBudgetReservationConflict) || errors.Is(err, workflow.ErrBaseBudgetLimitsInvalid) {
		return false
	}
	var exhausted *adk.RetryExhaustedError
	if errors.As(err, &exhausted) {
		return isRetryable(exhausted.LastErr)
	}
	var apiErr *modelopenai.APIError
	if errors.As(err, &apiErr) {
		return apiErr.HTTPStatusCode == http.StatusRequestTimeout || apiErr.HTTPStatusCode == http.StatusConflict ||
			apiErr.HTTPStatusCode == http.StatusTooManyRequests || apiErr.HTTPStatusCode >= http.StatusInternalServerError
	}
	var networkErr net.Error
	if errors.As(err, &networkErr) {
		return true
	}
	return true
}
