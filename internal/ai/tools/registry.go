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
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"SentinelOps/internal/ai/policy"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

var (
	// mu 保护 registry 的并发读写：注册时写锁（仅在 init 阶段），查询时读锁（高频并发）
	mu sync.RWMutex
	// registry 全局工具映射表，key 为工具名称字符串，value 为工具实例
	registry = make(map[string]tool.BaseTool)
	// registrations 记录名称的注册次数，使 strict durable 路径可拒绝覆盖过的重名。
	registrations = make(map[string]int)
)

// Register 将工具注册到全局注册表（通常在 init() 中调用，线程安全）。
// legacy 查询仍读取最后注册的实例；durable GetManyRequired 会拒绝任何重名注册。
func Register(name string, t tool.BaseTool) {
	mu.Lock()
	defer mu.Unlock()
	registrations[name]++
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

// GetManyRequired 严格解析 durable Tool，并校验 Catalog、名称和参数 Schema。
// 任何缺失、请求重名、注册重名、ToolInfo.Name 或 schema hash 漂移都会阻止启动。
func GetManyRequired(names []string) ([]tool.BaseTool, error) {
	seen := make(map[string]struct{}, len(names))
	result := make([]tool.BaseTool, 0, len(names))
	mu.RLock()
	defer mu.RUnlock()
	for _, name := range names {
		if _, duplicate := seen[name]; duplicate {
			return nil, fmt.Errorf("duplicate required tool %q", name)
		}
		seen[name] = struct{}{}
		instance, ok := registry[name]
		if !ok || instance == nil {
			return nil, fmt.Errorf("required tool %q is missing", name)
		}
		if registrations[name] != 1 {
			return nil, fmt.Errorf("required tool %q registered %d times", name, registrations[name])
		}
		entry, err := policy.LookupCatalog(name)
		if err != nil {
			return nil, err
		}
		if entry.Audience != policy.AudienceDurable {
			return nil, fmt.Errorf("required tool %q is restricted to audience %q", name, entry.Audience)
		}
		info, err := instance.Info(context.Background())
		if err != nil {
			return nil, fmt.Errorf("required tool %q Info: %w", name, err)
		}
		if info == nil || info.Name != name {
			actual := "<nil>"
			if info != nil {
				actual = info.Name
			}
			return nil, fmt.Errorf("required tool %q ToolInfo.Name = %q", name, actual)
		}
		hash, err := policy.ToolSchemaHash(info)
		if err != nil {
			return nil, fmt.Errorf("required tool %q schema hash: %w", name, err)
		}
		if hash != entry.SchemaHash {
			return nil, fmt.Errorf("required tool %q schema hash = %s, want %s", name, hash, entry.SchemaHash)
		}
		result = append(result, stableInfoTool(instance))
	}
	if len(result) != len(names) {
		return nil, fmt.Errorf("required tool count = %d, want %d", len(result), len(names))
	}
	return result, nil
}

// stableInfoTool isolates the Registry's canonical ToolInfo from framework
// adapters. Eino model adapters may normalize JSON Schema in place (for
// example, sorting nested required fields); returning a deep copy keeps the
// registered schema and its catalog hash immutable across Agent lifecycles.
func stableInfoTool(instance tool.BaseTool) tool.BaseTool {
	switch typed := instance.(type) {
	case tool.EnhancedStreamableTool:
		return &stableEnhancedStreamableTool{EnhancedStreamableTool: typed}
	case tool.EnhancedInvokableTool:
		return &stableEnhancedInvokableTool{EnhancedInvokableTool: typed}
	case tool.StreamableTool:
		return &stableStreamableTool{StreamableTool: typed}
	case tool.InvokableTool:
		return &stableInvokableTool{InvokableTool: typed}
	default:
		return &stableBaseTool{BaseTool: instance}
	}
}

func stableToolInfo(ctx context.Context, source tool.BaseTool) (*schema.ToolInfo, error) {
	info, err := source.Info(ctx)
	if err != nil || info == nil {
		return info, err
	}
	raw, err := json.Marshal(info)
	if err != nil {
		return nil, fmt.Errorf("clone ToolInfo: %w", err)
	}
	var clone schema.ToolInfo
	if err := json.Unmarshal(raw, &clone); err != nil {
		return nil, fmt.Errorf("clone ToolInfo: %w", err)
	}
	return &clone, nil
}

type stableBaseTool struct{ tool.BaseTool }

func (t *stableBaseTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return stableToolInfo(ctx, t.BaseTool)
}

type stableInvokableTool struct{ tool.InvokableTool }

func (t *stableInvokableTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return stableToolInfo(ctx, t.InvokableTool)
}

type stableStreamableTool struct{ tool.StreamableTool }

func (t *stableStreamableTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return stableToolInfo(ctx, t.StreamableTool)
}

type stableEnhancedInvokableTool struct{ tool.EnhancedInvokableTool }

func (t *stableEnhancedInvokableTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return stableToolInfo(ctx, t.EnhancedInvokableTool)
}

type stableEnhancedStreamableTool struct{ tool.EnhancedStreamableTool }

func (t *stableEnhancedStreamableTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return stableToolInfo(ctx, t.EnhancedStreamableTool)
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
