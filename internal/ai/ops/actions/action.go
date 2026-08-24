// Package actions AI 运维动作执行器。
//
// 定义统一的 Executor 接口，所有动作类型实现同一接口，
// 引擎通过 Registry 按 action_type 字符串查找并调用。
package actions

import "context"

// ActionResult 动作执行结果
type ActionResult struct {
	Success bool
	Output  map[string]string // 供后续步骤通过 {{steps.N.output.xxx}} 引用
	Message string
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
