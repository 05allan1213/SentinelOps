package models

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"SentinelOps/internal/ai/breaker"
	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
	appconfig "SentinelOps/internal/config"

	modelopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestRetrySuccessAndExhaustedFailover(t *testing.T) {
	t.Run("retry success", func(t *testing.T) {
		transient := errors.New("temporary provider failure")
		first := &scriptedModel{generate: []scriptedResult{{err: transient}, {message: schema.AssistantMessage("retry ok", nil)}}}
		second := &scriptedModel{generate: []scriptedResult{{message: schema.AssistantMessage("unexpected", nil)}}}
		err := runReliabilityAgent(t, first, second, false)
		if err != nil {
			t.Fatal(err)
		}
		if first.GenerateCalls() != 2 || second.GenerateCalls() != 0 {
			t.Fatalf("calls = first %d second %d, want 2/0", first.GenerateCalls(), second.GenerateCalls())
		}
	})

	t.Run("retry exhausted then failover success", func(t *testing.T) {
		transient := errors.New("temporary provider failure")
		first := &scriptedModel{generate: []scriptedResult{{err: transient}, {err: transient}}}
		second := &scriptedModel{generate: []scriptedResult{{message: schema.AssistantMessage("failover ok", nil)}}}
		err := runReliabilityAgent(t, first, second, false)
		if err != nil {
			t.Fatal(err)
		}
		if first.GenerateCalls() != 2 || second.GenerateCalls() != 1 {
			t.Fatalf("calls = first %d second %d, want 2/1", first.GenerateCalls(), second.GenerateCalls())
		}
	})

	t.Run("failover exhausted", func(t *testing.T) {
		transient := errors.New("temporary provider failure")
		first := &scriptedModel{generate: []scriptedResult{{err: transient}, {err: transient}}}
		second := &scriptedModel{generate: []scriptedResult{{err: transient}, {err: transient}}}
		err := runReliabilityAgent(t, first, second, false)
		if err == nil {
			t.Fatal("exhausted reliability call succeeded")
		}
		if first.GenerateCalls() != 2 || second.GenerateCalls() != 2 {
			t.Fatalf("calls = first %d second %d, want 2/2", first.GenerateCalls(), second.GenerateCalls())
		}
	})
}

func TestRetryRejectsAuthenticationParameterPolicyAndBudgetErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "authentication", err: &modelopenai.APIError{HTTPStatusCode: 401, Message: "unauthorized"}},
		{name: "parameter", err: &modelopenai.APIError{HTTPStatusCode: 400, Message: "invalid parameter"}},
		{name: "policy", err: &policy.ToolPolicyError{Code: policy.PolicyMutationDisabled, ToolName: "block_ip", Risk: policy.RiskL2}},
		{name: "budget", err: workflow.ErrBaseBudgetExhausted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first := &scriptedModel{generate: []scriptedResult{{err: tt.err}}}
			second := &scriptedModel{generate: []scriptedResult{{message: schema.AssistantMessage("must not run", nil)}}}
			if err := runReliabilityAgent(t, first, second, false); err == nil {
				t.Fatal("non-retryable error succeeded")
			}
			if first.GenerateCalls() != 1 || second.GenerateCalls() != 0 {
				t.Fatalf("calls = first %d second %d, want 1/0", first.GenerateCalls(), second.GenerateCalls())
			}
		})
	}
}

func TestNoDoubleRetryUsesOnlyOfficialRetryAndFailover(t *testing.T) {
	transient := errors.New("temporary provider failure")
	first := &scriptedModel{generate: []scriptedResult{{err: transient}, {err: transient}}}
	second := &scriptedModel{generate: []scriptedResult{{message: schema.AssistantMessage("failover ok", nil)}}}
	if err := runReliabilityAgent(t, first, second, false); err != nil {
		t.Fatal(err)
	}
	if first.GenerateCalls()+second.GenerateCalls() != 3 {
		t.Fatalf("physical calls = %d, want 3 without a project retry multiplier", first.GenerateCalls()+second.GenerateCalls())
	}
}

func TestBreakerSharedAcrossReliabilityBuilds(t *testing.T) {
	cfg, err := appconfig.Parse([]byte(appconfigCandidatesTestConfig))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ModelReliability = appconfig.ModelReliability{
		Retry:   appconfig.ModelRetry{MaxRetries: 1, BaseBackoffMS: 1},
		Breaker: appconfig.ModelBreaker{FailureThreshold: 100, OpenTimeoutMS: 60000},
		Limiter: appconfig.ModelLimiter{QPS: 10000, Burst: 10000},
	}
	factory := func(context.Context, chatProfile) (model.BaseChatModel, error) { return &scriptedModel{}, nil }
	binder := func(_ CandidateIdentity, endpoint model.BaseChatModel) (model.BaseChatModel, error) {
		return endpoint, nil
	}
	first, err := buildReliability(context.Background(), cfg, "default", binder, factory)
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildReliability(context.Background(), cfg, "default", binder, factory)
	if err != nil {
		t.Fatal(err)
	}
	firstCandidate, firstOK := first.initial.(*candidateModel)
	secondCandidate, secondOK := second.initial.(*candidateModel)
	if !firstOK || !secondOK || firstCandidate.health != secondCandidate.health {
		t.Fatal("separate reliability builds did not share process breaker state")
	}
}

func TestStreamFailureTripsBreaker(t *testing.T) {
	registry := breaker.NewRegistry(breaker.Settings{FailureThreshold: 1, OpenTimeout: time.Hour})
	health, err := registry.For("provider_a/stream")
	if err != nil {
		t.Fatal(err)
	}
	wrapped := &candidateModel{
		endpoint: &scriptedModel{streams: []scriptedStream{{err: errors.New("stream interrupted")}}},
		health:   health,
	}
	stream, err := wrapped.Stream(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = stream.Recv(); err == nil {
		t.Fatal("stream read failure was not returned")
	}
	if err = health.Allow(); !errors.Is(err, breaker.ErrOpen) {
		t.Fatalf("breaker error = %v, want ErrOpen", err)
	}
}

func TestConfigureChatModelAgentSetsOfficialConfigsExactlyOnce(t *testing.T) {
	reliability := &Reliability{
		initial: &scriptedModel{},
		retry:   &adk.ModelRetryConfig{MaxRetries: 2},
		failover: &adk.ModelFailoverConfig[*schema.Message]{
			MaxRetries:     1,
			ShouldFailover: func(context.Context, *schema.Message, error) bool { return true },
			GetFailoverModel: func(context.Context, *adk.FailoverContext[*schema.Message]) (model.BaseChatModel, []*schema.Message, error) {
				return &scriptedModel{}, nil, nil
			},
		},
	}
	config := &adk.ChatModelAgentConfig{}
	if err := ConfigureChatModelAgent(config, reliability); err != nil {
		t.Fatal(err)
	}
	if config.Model != reliability.initial || config.ModelRetryConfig != reliability.retry || config.ModelFailoverConfig != reliability.failover {
		t.Fatal("official model, retry or failover configuration was not installed")
	}
	if err := ConfigureChatModelAgent(config, reliability); err == nil {
		t.Fatal("duplicate reliability configuration succeeded")
	}
}

func TestFailoverContinuesAfterLastSuccessfulCandidate(t *testing.T) {
	cfg, err := appconfig.Parse([]byte(appconfigCandidatesTestConfig))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ModelReliability = appconfig.ModelReliability{
		Retry:   appconfig.ModelRetry{MaxRetries: 1, BaseBackoffMS: 1},
		Breaker: appconfig.ModelBreaker{FailureThreshold: 100, OpenTimeoutMS: 60000},
		Limiter: appconfig.ModelLimiter{QPS: 10000, Burst: 10000},
	}
	first, second := &scriptedModel{}, &scriptedModel{}
	byRef := map[string]model.BaseChatModel{"provider_a/chat": first, "provider_b/chat": second}
	reliability, err := buildReliability(context.Background(), cfg, "default",
		func(_ CandidateIdentity, endpoint model.BaseChatModel) (model.BaseChatModel, error) {
			return endpoint, nil
		},
		func(_ context.Context, candidate chatProfile) (model.BaseChatModel, error) {
			return byRef[candidate.CatalogRef], nil
		})
	if err != nil {
		t.Fatal(err)
	}
	selected, _, err := reliability.failover.GetFailoverModel(context.Background(), &adk.FailoverContext[*schema.Message]{
		FailoverAttempt: 1,
		LastErr: &adk.RetryExhaustedError{LastErr: &candidateCallError{
			order: 1,
			err:   errors.New("last successful candidate failed"),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	selectedCandidate, ok := selected.(*candidateModel)
	if !ok || selectedCandidate.endpoint != first {
		t.Fatalf("candidate after order 1 = %#v, want ordered wrap to candidate 0", selected)
	}
}

func TestStreamFailureTriggersRetry(t *testing.T) {
	first := &scriptedModel{streams: []scriptedStream{
		{messages: []*schema.Message{schema.AssistantMessage("partial", nil)}, err: errors.New("stream interrupted")},
		{messages: []*schema.Message{schema.AssistantMessage("complete", nil)}},
	}}
	second := &scriptedModel{}
	if err := runReliabilityAgent(t, first, second, true); err != nil {
		t.Fatal(err)
	}
	if first.StreamCalls() != 2 || second.StreamCalls() != 0 {
		t.Fatalf("stream calls = first %d second %d, want 2/0", first.StreamCalls(), second.StreamCalls())
	}
}

func runReliabilityAgent(t *testing.T, first, second *scriptedModel, streaming bool) error {
	t.Helper()
	cfg, err := appconfig.Parse([]byte(appconfigCandidatesTestConfig))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ModelReliability = appconfig.ModelReliability{
		Retry:   appconfig.ModelRetry{MaxRetries: 1, BaseBackoffMS: 1},
		Breaker: appconfig.ModelBreaker{FailureThreshold: 100, OpenTimeoutMS: 60000},
		Limiter: appconfig.ModelLimiter{QPS: 10000, Burst: 10000},
	}
	byRef := map[string]model.BaseChatModel{"provider_a/chat": first, "provider_b/chat": second}
	reliability, err := buildReliability(context.Background(), cfg, "default",
		func(_ CandidateIdentity, endpoint model.BaseChatModel) (model.BaseChatModel, error) {
			return endpoint, nil
		},
		func(_ context.Context, candidate chatProfile) (model.BaseChatModel, error) {
			return byRef[candidate.CatalogRef], nil
		})
	if err != nil {
		t.Fatal(err)
	}
	agentConfig := &adk.ChatModelAgentConfig{
		Name: "reliability_test", Description: "reliability contract",
		GenModelInput: func(_ context.Context, _ string, input *adk.AgentInput) ([]adk.Message, error) {
			return input.Messages, nil
		},
	}
	if err = ConfigureChatModelAgent(agentConfig, reliability); err != nil {
		t.Fatal(err)
	}
	agent, err := adk.NewChatModelAgent(context.Background(), agentConfig)
	if err != nil {
		t.Fatal(err)
	}
	iterator := agent.Run(context.Background(), &adk.AgentInput{Messages: []adk.Message{schema.UserMessage("test")}, EnableStreaming: streaming})
	for {
		event, ok := iterator.Next()
		if !ok {
			return nil
		}
		if event.Err != nil {
			return event.Err
		}
		if event.Output != nil && event.Output.MessageOutput != nil && event.Output.MessageOutput.MessageStream != nil {
			for {
				_, receiveErr := event.Output.MessageOutput.MessageStream.Recv()
				if errors.Is(receiveErr, io.EOF) {
					break
				}
				if receiveErr != nil {
					// Eino 会把失败轮次作为可观测 stream event 发出，终态错误由 event.Err 返回。
					break
				}
			}
		}
	}
}

type scriptedResult struct {
	message *schema.Message
	err     error
}

type scriptedStream struct {
	messages []*schema.Message
	err      error
}

type scriptedModel struct {
	mu            sync.Mutex
	generate      []scriptedResult
	streams       []scriptedStream
	generateCalls int
	streamCalls   int
}

func (m *scriptedModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	index := m.generateCalls
	m.generateCalls++
	if index >= len(m.generate) {
		return nil, errors.New("unexpected scripted Generate call")
	}
	return m.generate[index].message, m.generate[index].err
}

func (m *scriptedModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	m.mu.Lock()
	index := m.streamCalls
	m.streamCalls++
	if index >= len(m.streams) {
		m.mu.Unlock()
		return nil, errors.New("unexpected scripted Stream call")
	}
	result := m.streams[index]
	m.mu.Unlock()
	reader, writer := schema.Pipe[*schema.Message](1)
	go func() {
		defer writer.Close()
		for _, message := range result.messages {
			if writer.Send(message, nil) {
				return
			}
		}
		if result.err != nil {
			writer.Send(nil, result.err)
		}
	}()
	return reader, nil
}

func (m *scriptedModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}

func (m *scriptedModel) GenerateCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.generateCalls
}

func (m *scriptedModel) StreamCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.streamCalls
}
