package milvus

import (
	"context"
	"fmt"
	"strings"
)

// DeleteEventsByIDs 按事件 ID 批量删除 Milvus 向量，分批执行防止过滤表达式过长。
// 每批最多 batchSize 条（默认 100），单批失败立即返回错误。
// 调用方（如订阅删除）应在业务层决定是否继续执行后续操作。
func DeleteEventsByIDs(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	cli, err := GetClient(ctx)
	if err != nil {
		return fmt.Errorf("连接 Milvus 失败: %w", err)
	}
	const batchSize = 100
	for i := 0; i < len(ids); i += batchSize {
		end := i + batchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[i:end]
		// 构建 Milvus 过滤表达式：id in ["id1", "id2", ...]
		quoted := make([]string, len(batch))
		for j, id := range batch {
			quoted[j] = `"` + id + `"`
		}
		expr := fmt.Sprintf("id in [%s]", strings.Join(quoted, ", "))
		if err = cli.Delete(ctx, CollectionName, "", expr); err != nil {
			return fmt.Errorf("删除第 %d-%d 批向量失败: %w", i+1, end, err)
		}
	}
	return nil
}
