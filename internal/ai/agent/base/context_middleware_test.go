package base

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type emptySummaryModel struct{}

func (m *emptySummaryModel) Generate(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	return schema.AssistantMessage("", nil), nil
}

func (m *emptySummaryModel) Stream(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("empty summary model does not support streaming")
}

func TestContextGovernanceSurvivesEmptySummaryModel(t *testing.T) {
	middlewares, err := NewContextGovernanceMiddleware(context.Background(), &emptySummaryModel{})
	if err != nil {
		t.Fatalf("NewContextGovernanceMiddleware: %v", err)
	}
	if len(middlewares) == 0 {
		t.Fatal("context governance middleware is required")
	}
	messages := make([]*schema.Message, 0, 41)
	for index := 0; index < 41; index++ {
		messages = append(messages, schema.UserMessage(fmt.Sprintf("上下文消息 %d", index)))
	}
	state := &adk.ChatModelAgentState{Messages: messages}
	_, next, err := middlewares[0].BeforeModelRewriteState(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("empty summary model must not fail the run: %v", err)
	}
	if next == nil || len(next.Messages) == 0 {
		t.Fatal("summarization must return rewritten messages")
	}
	foundFallback := false
	for _, message := range next.Messages {
		if message != nil && strings.Contains(message.Content, "摘要模型本轮未返回可用内容") {
			foundFallback = true
			break
		}
	}
	if !foundFallback {
		t.Fatalf("fallback summary is missing: %+v", next.Messages)
	}
}

func TestSummaryModelReturnedEmptyTreatsEmptyAssistantAsRetryable(t *testing.T) {
	cases := []struct {
		name     string
		response *schema.Message
		err      error
		want     bool
	}{
		{name: "nil response", want: true},
		{name: "transport error", err: errors.New("boom"), want: true},
		{name: "empty assistant", response: schema.AssistantMessage("", nil), want: true},
		{name: "non assistant content", response: schema.UserMessage("text"), want: true},
		{name: "non empty assistant", response: schema.AssistantMessage("summary", nil), want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := summaryModelReturnedEmpty(context.Background(), tc.response, tc.err); got != tc.want {
				t.Fatalf("summaryModelReturnedEmpty = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFallbackSummaryTextKeepsRecentContextAndStaysBounded(t *testing.T) {
	messages := []*schema.Message{
		schema.UserMessage(strings.Repeat("最旧上下文", 400)),
		nil,
		schema.AssistantMessage(strings.Repeat("较早摘要", 200), nil),
		schema.UserMessage("最近一条关键上下文 EV-CHAIN-77"),
	}
	summary := fallbackSummaryText(messages)
	if strings.TrimSpace(summary) == "" {
		t.Fatal("fallback summary must not be empty")
	}
	if !strings.Contains(summary, "EV-CHAIN-77") {
		t.Fatalf("fallback summary must keep the most recent context: %q", summary)
	}
	if len([]rune(summary)) > fallbackSummaryMaxRunes+fallbackMessageMaxRunes {
		t.Fatalf("fallback summary is not bounded: %d runes", len([]rune(summary)))
	}
	if got := fallbackSummaryText(nil); strings.TrimSpace(got) == "" {
		t.Fatal("empty context must still produce a non-empty fallback summary")
	}
}

func TestStaticSummaryModelAlwaysReturnsNonEmptySummary(t *testing.T) {
	model := &staticSummaryModel{content: "fallback summary"}
	message, err := model.Generate(context.Background(), nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if message == nil || message.Role != schema.Assistant || strings.TrimSpace(message.Content) == "" {
		t.Fatalf("static summary model returned unusable message: %+v", message)
	}
	if _, err := model.Stream(context.Background(), nil); err == nil {
		t.Fatal("static summary model must not pretend to support streaming")
	}
}
