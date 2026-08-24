package system

import (
	"context"
	"encoding/json"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/gogf/gf/v2/frame/g"
)

// GetCurrentTimeInput 获取当前时间的输入参数（无需输入）
type GetCurrentTimeInput struct {
	// 无需输入参数
}

// GetCurrentTimeOutput 获取当前时间的输出结果
type GetCurrentTimeOutput struct {
	Success      bool   `json:"success" jsonschema:"description=Indicates whether the time retrieval was successful"`
	Seconds      int64  `json:"seconds" jsonschema:"description=Current Unix timestamp in seconds since epoch (1970-01-01 00:00:00 UTC)"`
	Milliseconds int64  `json:"milliseconds" jsonschema:"description=Current Unix timestamp in milliseconds since epoch (1970-01-01 00:00:00 UTC)"`
	Microseconds int64  `json:"microseconds" jsonschema:"description=Current Unix timestamp in microseconds since epoch (1970-01-01 00:00:00 UTC)"`
	Timestamp    string `json:"timestamp" jsonschema:"description=Human-readable timestamp in format 'YYYY-MM-DD HH:MM:SS.microseconds'"`
	Message      string `json:"message" jsonschema:"description=Status message describing the operation result"`
}

// NewGetCurrentTimeTool 创建获取当前时间的工具
func NewGetCurrentTimeTool() tool.InvokableTool {
	t, err := utils.InferOptionableTool(
		"get_current_time",
		"Get current system time in multiple formats. Returns the current time in seconds (Unix timestamp), milliseconds, and microseconds. Use this tool when you need to retrieve current system time for logging, timing operations, or timestamping events.",
		func(ctx context.Context, input *GetCurrentTimeInput, opts ...tool.Option) (output string, err error) {
			now := time.Now()
			seconds := now.Unix()
			milliseconds := now.UnixMilli()
			microseconds := now.UnixMicro()
			timestamp := now.Format("2006-01-02 15:04:05")

			g.Log().Debugf(ctx, "[Tool] get_current_time | timestamp=%s", timestamp)

			timeOutput := GetCurrentTimeOutput{
				Success:      true,
				Seconds:      seconds,
				Milliseconds: milliseconds,
				Microseconds: microseconds,
				Timestamp:    timestamp,
				Message:      "Current time retrieved successfully",
			}

			jsonBytes, err := json.MarshalIndent(timeOutput, "", "  ")
			if err != nil {
				g.Log().Warningf(ctx, "[Tool] get_current_time 序列化失败: %v", err)
				return "", err
			}
			return string(jsonBytes), nil
		})

	if err != nil {
		panic(err)
	}

	return t
}
