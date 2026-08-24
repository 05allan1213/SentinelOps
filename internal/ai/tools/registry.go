// Package tools 全局工具注册表：供各 Agent pipeline 共享工具实例。
//
// 设计原则：
//   - 工具是无状态的（只封装数据库/API 调用逻辑），可安全地在多个 Agent 执行器之间共享
//   - 注册一次，全局复用，避免每个 pipeline 各自 New 实例导致内存浪费
//   - 读写锁（sync.RWMutex）保证并发安全：注册时写锁，查询时读锁，不阻塞并发请求
//
// 使用方式：
//  1. 在 init.go 的 init() 中调用 Register 完成所有工具的一次性注册
//  2. Agent pipeline 通过 GetMany([]string{...}) 按名称获取所需工具子集
package tools

import (
	"sync"

	"github.com/cloudwego/eino/components/tool"
)

var (
	// mu 保护 registry 的并发读写：注册时写锁（仅在 init 阶段），查询时读锁（高频并发）
	mu sync.RWMutex
	// registry 全局工具映射表，key 为工具名称字符串，value 为工具实例
	registry = make(map[string]tool.BaseTool)
)

// Register 将工具注册到全局注册表（通常在 init() 中调用，线程安全）。
// 若同名工具已存在，新注册的工具会覆盖旧的（允许测试时替换为 mock 实现）。
func Register(name string, t tool.BaseTool) {
	mu.Lock()
	defer mu.Unlock()
	registry[name] = t
}

// Get 按名称从注册表获取工具；不存在时返回 nil。
// 调用方应检查返回值是否为 nil，避免空指针。
func Get(name string) tool.BaseTool {
	mu.RLock()
	defer mu.RUnlock()
	return registry[name]
}

// GetMany 按名称列表批量获取工具，静默跳过未注册的名称。
//
// 设计说明：静默跳过而非返回 error，是因为工具名称配置错误属于开发期问题，
// 部署后不应因工具名拼写错误导致 Agent 整体无法初始化。
// pipeline 通过此函数按声明的 ToolNames 取得所需工具子集，实现工具隔离。
func GetMany(names []string) []tool.BaseTool {
	mu.RLock()
	defer mu.RUnlock()
	result := make([]tool.BaseTool, 0, len(names))
	for _, name := range names {
		if t, ok := registry[name]; ok {
			result = append(result, t)
		}
	}
	return result
}

// All 返回所有已注册工具的快照（调试用）。
// 返回副本而非原始 map，防止调用方意外修改注册表。
func All() map[string]tool.BaseTool {
	mu.RLock()
	defer mu.RUnlock()
	snapshot := make(map[string]tool.BaseTool, len(registry))
	for k, v := range registry {
		snapshot[k] = v
	}
	return snapshot
}
