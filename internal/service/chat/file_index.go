// file_index.go 知识库索引构建逻辑（向后兼容 /api/chat/v1/upload 接口）。
// 调用 knowledge service 将文件注册到默认知识库并异步索引。
package chatsvc

import (
	"context"
	"fmt"

	aidoc "SentinelOps/internal/ai/document"
	knowledgesvc "SentinelOps/internal/service/knowledge"
)

// BuildFileIndex 对指定路径的文件执行知识库索引（向后兼容旧接口）。
// 文件已由上层（HTTP 控制器）保存到 path，此函数将其注册到默认知识库并异步索引。
func BuildFileIndex(ctx context.Context, path string, config aidoc.ChunkConfig) error {
	_, err := knowledgesvc.RegisterExistingFile(ctx, "default", path, config)
	if err != nil {
		return fmt.Errorf("register file index: %w", err)
	}
	return nil
}
