// Package actions AI 运维动作执行器。
//
// 定义统一的 Executor 接口，所有动作类型实现同一接口，
// 引擎通过 Registry 按 action_type 字符串查找并调用。
package actions

import (
	"context"
	"fmt"
)

// ActionResult 动作执行结果
type ActionResult struct {
	Success bool
	Output  map[string]string // 供后续步骤通过 {{steps.N.output.xxx}} 引用
	Message string
}

// TargetState 是对账查询返回的脱敏目标事实；查询不执行任何动作。
type TargetState struct {
	Known    bool
	Applied  bool
	Evidence map[string]any
}

// TargetStateQuerier 是现有 Action 的可选对账能力，不是第二套 Registry。
type TargetStateQuerier interface {
	QueryTargetState(context.Context, map[string]string) (TargetState, error)
}

// Executor 动作执行器接口
type Executor interface {
	Execute(ctx context.Context, params map[string]string) (ActionResult, error)
	Name() string
}

var registry = map[string]Executor{}

// Register 注册动作执行器
func Register(e Executor) {
	registry[e.Name()] = e
}

// Get 按 action_type 获取执行器
func Get(actionType string) (Executor, bool) {
	e, ok := registry[actionType]
	return e, ok
}

// QueryTargetState 复用已注册 Action 的目标查询实现。
func QueryTargetState(ctx context.Context, actionType string, params map[string]string) (TargetState, error) {
	exec, ok := Get(actionType)
	if !ok {
		return TargetState{}, fmt.Errorf("action %q is not registered", actionType)
	}
	querier, ok := exec.(TargetStateQuerier)
	if !ok {
		return TargetState{}, fmt.Errorf("action %q does not expose target state", actionType)
	}
	return querier.QueryTargetState(ctx, params)
}
