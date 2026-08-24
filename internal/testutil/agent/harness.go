// Package agenttest 提供只实现 Eino 公共接口的确定性 Agent 测试 double。
package agenttest

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// ErrScriptExhausted 表示测试执行次数超过 scripted step 数量。
var ErrScriptExhausted = errors.New("script exhausted")

// CallMode 标识 scripted double 的同步或流式调用入口。
type CallMode string

const (
	// CallModeGenerate 标识 ChatModel.Generate 调用。
	CallModeGenerate CallMode = "generate"
	// CallModeStream 标识 ChatModel.Stream 调用。
	CallModeStream CallMode = "stream"
	// CallModeInvoke 标识 Tool.InvokableRun 调用。
	CallModeInvoke CallMode = "invoke"
	// CallModeToolStream 标识 Tool.StreamableRun 调用。
	CallModeToolStream CallMode = "tool_stream"
)

// ModelStep 描述一次 ChatModel 调用的确定性结果。
type ModelStep struct {
	Message *schema.Message
	Chunks  []*schema.Message
	Err     error
}

// ModelCall 记录一次 ChatModel 公共接口调用。
type ModelCall struct {
	Mode      CallMode
	Input     []*schema.Message
	ToolNames []string
}

type scriptedModelState struct {
	mu    sync.Mutex
	steps []ModelStep
	next  int
	calls []ModelCall
}

// ScriptedChatModel 按顺序返回 ModelStep，并直接实现 Eino ToolCallingChatModel。
type ScriptedChatModel struct {
	state     *scriptedModelState
	toolNames []string
}

// NewScriptedChatModel 创建并发安全的确定性 ChatModel 测试 double。
func NewScriptedChatModel(steps ...ModelStep) *ScriptedChatModel {
	return &ScriptedChatModel{state: &scriptedModelState{steps: append([]ModelStep(nil), steps...)}}
}

// WithTools 返回绑定 ToolInfo 的不可变视图，并与原实例共享调用脚本和记录。
func (m *ScriptedChatModel) WithTools(infos []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	names := make([]string, 0, len(infos))
	for _, info := range infos {
		names = append(names, info.Name)
	}
	return &ScriptedChatModel{state: m.state, toolNames: names}, nil
}

// Generate 返回下一条同步 scripted 结果。
func (m *ScriptedChatModel) Generate(_ context.Context, input []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	step, err := m.nextStep(CallModeGenerate, input)
	if err != nil {
		return nil, err
	}
	if step.Err != nil {
		return nil, step.Err
	}
	if step.Message != nil {
		return step.Message, nil
	}
	return schema.ConcatMessages(step.Chunks)
}

// Stream 返回下一条流式 scripted 结果，并可在 chunks 后注入终止错误。
func (m *ScriptedChatModel) Stream(_ context.Context, input []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	step, err := m.nextStep(CallModeStream, input)
	if err != nil {
		return nil, err
	}
	if step.Err != nil && len(step.Chunks) == 0 {
		return nil, step.Err
	}
	chunks := step.Chunks
	if len(chunks) == 0 && step.Message != nil {
		chunks = []*schema.Message{step.Message}
	}
	return messageStream(chunks, step.Err), nil
}

func (m *ScriptedChatModel) nextStep(mode CallMode, input []*schema.Message) (ModelStep, error) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	if m.state.next >= len(m.state.steps) {
		return ModelStep{}, ErrScriptExhausted
	}
	step := m.state.steps[m.state.next]
	m.state.next++
	m.state.calls = append(m.state.calls, ModelCall{
		Mode: mode, Input: append([]*schema.Message(nil), input...), ToolNames: append([]string(nil), m.toolNames...),
	})
	return step, nil
}

// Calls 返回当前 ChatModel 调用记录的副本。
func (m *ScriptedChatModel) Calls() []ModelCall {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	calls := make([]ModelCall, len(m.state.calls))
	for i, call := range m.state.calls {
		calls[i] = ModelCall{
			Mode:      call.Mode,
			Input:     append([]*schema.Message(nil), call.Input...),
			ToolNames: append([]string(nil), call.ToolNames...),
		}
	}
	return calls
}

// ToolStep 描述一次 Tool 调用的确定性结果。
type ToolStep struct {
	Result string
	Chunks []string
	Err    error
	Run    func(context.Context, string) (string, error)
}

// ToolCall 记录一次 Tool 公共接口调用。
type ToolCall struct {
	Mode      CallMode
	Arguments string
}

type scriptedToolState struct {
	mu    sync.Mutex
	steps []ToolStep
	next  int
	calls []ToolCall
}

// ScriptedTool 按顺序返回 ToolStep，并直接实现 Eino InvokableTool 和 StreamableTool。
type ScriptedTool struct {
	name        string
	description string
	state       *scriptedToolState
}

// NewScriptedTool 创建确定性 Tool 测试 double。
func NewScriptedTool(name, description string, steps ...ToolStep) *ScriptedTool {
	return &ScriptedTool{name: name, description: description, state: &scriptedToolState{steps: append([]ToolStep(nil), steps...)}}
}

// InterruptToolStep 使用 Eino 官方 CompositeInterrupt 表达可恢复测试中断。
func InterruptToolStep(info, state any, subInterrupts ...error) ToolStep {
	return ToolStep{Run: func(ctx context.Context, _ string) (string, error) {
		return "", tool.CompositeInterrupt(ctx, info, state, subInterrupts...)
	}}
}

// Info 返回测试 Tool 的 Eino ToolInfo。
func (t *ScriptedTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name, Desc: t.description}, nil
}

// InvokableRun 返回下一条同步 scripted 结果。
func (t *ScriptedTool) InvokableRun(ctx context.Context, arguments string, _ ...tool.Option) (string, error) {
	step, err := t.nextStep(CallModeInvoke, arguments)
	if err != nil {
		return "", err
	}
	if step.Run != nil {
		return step.Run(ctx, arguments)
	}
	if step.Err != nil {
		return "", step.Err
	}
	if step.Result != "" {
		return step.Result, nil
	}
	return strings.Join(step.Chunks, ""), nil
}

// StreamableRun 返回下一条流式 scripted 结果。
func (t *ScriptedTool) StreamableRun(ctx context.Context, arguments string, _ ...tool.Option) (*schema.StreamReader[string], error) {
	step, err := t.nextStep(CallModeToolStream, arguments)
	if err != nil {
		return nil, err
	}
	if step.Run != nil {
		result, runErr := step.Run(ctx, arguments)
		if runErr != nil {
			return nil, runErr
		}
		return stringStream([]string{result}, nil), nil
	}
	if step.Err != nil && len(step.Chunks) == 0 {
		return nil, step.Err
	}
	chunks := step.Chunks
	if len(chunks) == 0 && step.Result != "" {
		chunks = []string{step.Result}
	}
	return stringStream(chunks, step.Err), nil
}

func (t *ScriptedTool) nextStep(mode CallMode, arguments string) (ToolStep, error) {
	t.state.mu.Lock()
	defer t.state.mu.Unlock()
	if t.state.next >= len(t.state.steps) {
		return ToolStep{}, ErrScriptExhausted
	}
	step := t.state.steps[t.state.next]
	t.state.next++
	t.state.calls = append(t.state.calls, ToolCall{Mode: mode, Arguments: arguments})
	return step, nil
}

// Calls 返回当前 Tool 调用记录的副本。
func (t *ScriptedTool) Calls() []ToolCall {
	t.state.mu.Lock()
	defer t.state.mu.Unlock()
	calls := make([]ToolCall, len(t.state.calls))
	copy(calls, t.state.calls)
	return calls
}

func messageStream(chunks []*schema.Message, terminalErr error) *schema.StreamReader[*schema.Message] {
	reader, writer := schema.Pipe[*schema.Message](len(chunks) + 1)
	go func() {
		defer writer.Close()
		for _, chunk := range chunks {
			if writer.Send(chunk, nil) {
				return
			}
		}
		if terminalErr != nil {
			writer.Send(nil, terminalErr)
		}
	}()
	return reader
}

func stringStream(chunks []string, terminalErr error) *schema.StreamReader[string] {
	reader, writer := schema.Pipe[string](len(chunks) + 1)
	go func() {
		defer writer.Close()
		for _, chunk := range chunks {
			if writer.Send(chunk, nil) {
				return
			}
		}
		if terminalErr != nil {
			writer.Send("", terminalErr)
		}
	}()
	return reader
}
