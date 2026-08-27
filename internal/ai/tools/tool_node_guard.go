package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/cloudwego/eino/components/tool"
)

// UnknownToolHandler 把模型幻觉出的工具名转换为可恢复的 Tool 结果文本，
// 而不是让官方 ToolsNode 直接硬失败；未知工具永远不会被执行。
func UnknownToolHandler(names []string) func(context.Context, string, string) (string, error) {
	available := append([]string(nil), names...)
	sort.Strings(available)
	return func(_ context.Context, name, _ string) (string, error) {
		return fmt.Sprintf("tool %q does not exist; available tools: %s", name, strings.Join(available, ", ")), nil
	}
}

// ToolNames 提取 BaseTool 清单的工具名，供 UnknownToolHandler 生成可用列表。
func ToolNames(ctx context.Context, list []tool.BaseTool) ([]string, error) {
	names := make([]string, 0, len(list))
	for _, item := range list {
		if item == nil {
			continue
		}
		info, err := item.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("resolve tool name: %w", err)
		}
		if info == nil || strings.TrimSpace(info.Name) == "" {
			return nil, fmt.Errorf("tool info has no name")
		}
		names = append(names, info.Name)
	}
	return names, nil
}

// NormalizeTriggerOpsArguments 兼容部分模型把 proposals[].arguments_json
// 传成 JSON 对象而不是字符串的形态；trigger_ops 的契约仍是规范化后的 JSON
// 字符串，参数最终仍由服务端 Catalog 校验并 canonicalize。
func NormalizeTriggerOpsArguments(_ context.Context, name, args string) (string, error) {
	if name != "trigger_ops" || strings.TrimSpace(args) == "" {
		return args, nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(args), &raw); err != nil {
		return args, nil
	}
	proposals, ok := raw["proposals"]
	if !ok {
		return args, nil
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(proposals, &items); err != nil {
		return args, nil
	}
	changed := false
	for i := range items {
		value, present := items[i]["arguments_json"]
		if !present {
			continue
		}
		var text string
		if err := json.Unmarshal(value, &text); err == nil {
			continue
		}
		var object any
		if err := json.Unmarshal(value, &object); err != nil {
			continue
		}
		compact, err := json.Marshal(object)
		if err != nil {
			return args, nil
		}
		encoded, err := json.Marshal(string(compact))
		if err != nil {
			return args, nil
		}
		items[i]["arguments_json"] = json.RawMessage(encoded)
		changed = true
	}
	if !changed {
		return args, nil
	}
	raw["proposals"], _ = json.Marshal(items)
	rewritten, err := json.Marshal(raw)
	if err != nil {
		return args, nil
	}
	return string(rewritten), nil
}
