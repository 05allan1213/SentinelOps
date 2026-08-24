// Package actions AI 运维模板变量渲染器。
// 支持 {{event.xxx}} 和 {{steps.N.output.xxx}} 两类变量。
package actions

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// RenderContext 变量上下文（并发安全）
type RenderContext struct {
	Event       map[string]string         // event.xxx（只读，无需加锁）
	StepOutputs map[int]map[string]string // steps.N.output.xxx（N 从 1 开始）
	mu          sync.RWMutex
}

// SetStepOutput 线程安全地写入步骤输出
func (rc *RenderContext) SetStepOutput(order int, output map[string]string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.StepOutputs[order] = output
}

// RenderSnapshot 渲染用的只读快照（不含 mutex，可安全值传递）
type RenderSnapshot struct {
	Event       map[string]string
	StepOutputs map[int]map[string]string
}

// Snapshot 返回当前上下文的只读快照（用于渲染，不含 mutex）
func (rc *RenderContext) Snapshot() RenderSnapshot {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	snap := RenderSnapshot{
		Event:       rc.Event,
		StepOutputs: make(map[int]map[string]string, len(rc.StepOutputs)),
	}
	for k, v := range rc.StepOutputs {
		snap.StepOutputs[k] = v
	}
	return snap
}

var varPattern = regexp.MustCompile(`\{\{([^}]+)\}\}`)

// Render 将参数 map 中所有值的模板变量替换为实际值
func Render(params map[string]string, rc RenderSnapshot) map[string]string {
	result := make(map[string]string, len(params))
	for k, v := range params {
		result[k] = renderString(v, rc)
	}
	return result
}

func renderString(s string, rc RenderSnapshot) string {
	return varPattern.ReplaceAllStringFunc(s, func(match string) string {
		inner := strings.TrimSpace(match[2 : len(match)-2])
		parts := strings.SplitN(inner, ".", 4)
		switch parts[0] {
		case "event":
			if len(parts) == 2 {
				if v, ok := rc.Event[parts[1]]; ok {
					return v
				}
			}
		case "steps":
			// 格式：steps.N.output.key（4段）
			if len(parts) == 4 {
				var n int
				fmt.Sscanf(parts[1], "%d", &n)
				if outputs, ok := rc.StepOutputs[n]; ok {
					if v, ok2 := outputs[parts[3]]; ok2 {
						return v
					}
				}
			}
		}
		return match // 未找到变量保留原文
	})
}
