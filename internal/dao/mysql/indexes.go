package mysql

import (
	"context"

	"github.com/gogf/gf/v2/frame/g"
)

// CreateIndexes 创建性能优化索引，在 Init 成功后调用。
func CreateIndexes(ctx context.Context) error {
	db, err := DB(ctx)
	if err != nil {
		return err
	}

	// 索引列表：仅保留 GORM AutoMigrate 无法通过模型 tag 创建的索引。
	// 普通字段索引（severity、status、cve_id、parent_node_id 等）已通过 model.go
	// 中的 gorm:"index" tag 由 AutoMigrate 统一管理，无需在此重复创建。
	// DESC 索引：GORM 不支持通过 tag 指定排序方向，必须手动 DDL。
	indexes := []struct {
		name  string
		table string
		sql   string
	}{
		{"idx_events_created_at", "events", "CREATE INDEX idx_events_created_at ON events(created_at DESC)"},
		{"idx_trace_runs_created", "agent_trace_runs", "CREATE INDEX idx_trace_runs_created ON agent_trace_runs(created_at DESC)"},
	}

	for _, idx := range indexes {
		// 检查索引是否已存在
		var count int64
		db.Raw("SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?",
			idx.table, idx.name).Scan(&count)

		if count > 0 {
			continue
		}

		if err := db.Exec(idx.sql).Error; err != nil {
			g.Log().Warningf(ctx, "[MySQL] 创建索引 %s 失败: %v", idx.name, err)
		}
	}
	g.Log().Infof(ctx, "[MySQL] 索引创建完成")
	return nil
}
