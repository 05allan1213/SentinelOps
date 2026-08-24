package trace

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	dao "SentinelOps/internal/dao/mysql"
	"SentinelOps/utility/stringutil"

	"gorm.io/gorm"
)

// ── GORM Plugin：MySQL 慢查询追踪 ──────────────────────────────────────────────
//
// 背景：AI 系统的性能瓶颈常出现在数据库查询（如 RAG 检索前的事件过滤、报告生成时的聚合查询）。
// 传统日志只能看到 SQL 语句，无法关联到具体的 AI 请求链路，排查困难。
//
// 设计目标：
//  1. 自动采样：只记录超过阈值（默认 100ms）的慢查询，避免追踪表爆炸
//  2. 零侵入：通过 GORM Plugin 机制自动拦截所有 DB 操作，业务代码无需修改
//  3. 链路关联：慢查询作为 DB 类型节点挂到当前 trace 树中，与 LLM/Tool 节点并列展示
//
// 注册方式（main.go）：
//
//	dao.RegisterPlugin(trace.NewGORMPlugin())
//
// 工作原理：
//  1. Before 回调：在 GORM 执行 SQL 前记录开始时间（db.InstanceSet）
//  2. After 回调：计算耗时，若超过阈值则调用 StartSpan/FinishSpan 创建 DB 节点
//  3. 防递归：跳过 agent_trace_* 表的查询，避免追踪系统自身的写库操作被追踪
//
// 配置项（manifest/config/config.yaml）：
//
//	trace:
//	  record_sql: true              # 是否记录 SQL 原文（截断至 500 字符）
//	  db_slow_threshold_ms: 100     # 慢查询阈值（毫秒）
//
// 注意：record_sql=true 可能记录敏感数据（如用户查询词），生产环境建议关闭。

// GORMPlugin 实现 MySQL 慢查询追踪（>100ms 采样）
type GORMPlugin struct {
	slowThresholdMs int64
}

func NewGORMPlugin() gorm.Plugin {
	cfg := GetConfig()
	threshold := int64(100) // 默认 100ms
	if cfg.DBSlowThresholdMs > 0 {
		threshold = cfg.DBSlowThresholdMs
	}
	return &GORMPlugin{slowThresholdMs: threshold}
}

func (p *GORMPlugin) Name() string {
	return "trace:db"
}

func (p *GORMPlugin) Initialize(db *gorm.DB) error {
	// 注册 Before 回调：记录开始时间
	if err := db.Callback().Create().Before("gorm:create").Register("trace:before", p.makeBeforeCallback()); err != nil {
		return err
	}
	if err := db.Callback().Query().Before("gorm:query").Register("trace:before", p.makeBeforeCallback()); err != nil {
		return err
	}
	if err := db.Callback().Update().Before("gorm:update").Register("trace:before", p.makeBeforeCallback()); err != nil {
		return err
	}
	if err := db.Callback().Delete().Before("gorm:delete").Register("trace:before", p.makeBeforeCallback()); err != nil {
		return err
	}

	// 注册 After 回调：计算耗时并记录慢查询
	if err := db.Callback().Create().After("gorm:create").Register("trace:after", p.dbAfterCallback); err != nil {
		return err
	}
	if err := db.Callback().Query().After("gorm:query").Register("trace:after", p.dbAfterCallback); err != nil {
		return err
	}
	if err := db.Callback().Update().After("gorm:update").Register("trace:after", p.dbAfterCallback); err != nil {
		return err
	}
	if err := db.Callback().Delete().After("gorm:delete").Register("trace:after", p.dbAfterCallback); err != nil {
		return err
	}

	return nil
}

type dbStartTimeKey struct{}

func (p *GORMPlugin) makeBeforeCallback() func(*gorm.DB) {
	return func(db *gorm.DB) {
		db.InstanceSet("trace:start_time", time.Now())
	}
}

func (p *GORMPlugin) dbAfterCallback(db *gorm.DB) {
	// 防止递归：跳过 trace 表自身的查询
	if db.Statement != nil && db.Statement.Table != "" {
		if strings.HasPrefix(db.Statement.Table, "agent_trace_") {
			return
		}
	}

	startVal, ok := db.InstanceGet("trace:start_time")
	if !ok {
		return
	}
	startTime, ok := startVal.(time.Time)
	if !ok {
		return
	}

	durationMs := time.Since(startTime).Milliseconds()
	if durationMs < p.slowThresholdMs {
		return // 未达到慢查询阈值
	}

	ctx := db.Statement.Context
	at := Extract(ctx)
	if at == nil {
		return // 不在追踪上下文中
	}

	// 提取 SQL 操作类型和表名
	operation := "UNKNOWN"
	tableName := ""
	if db.Statement != nil {
		if db.Statement.Table != "" {
			tableName = db.Statement.Table
		}
		// 从 SQL 推断操作类型
		sql := strings.ToUpper(strings.TrimSpace(db.Statement.SQL.String()))
		if strings.HasPrefix(sql, "SELECT") {
			operation = "SELECT"
		} else if strings.HasPrefix(sql, "INSERT") {
			operation = "INSERT"
		} else if strings.HasPrefix(sql, "UPDATE") {
			operation = "UPDATE"
		} else if strings.HasPrefix(sql, "DELETE") {
			operation = "DELETE"
		}
	}

	// 记录 SQL（可选，默认关闭）
	sqlText := ""
	if GetConfig().RecordSQL && db.Statement != nil {
		sqlText = stringutil.TruncateRunes(db.Statement.SQL.String(), 500)
	}

	// 创建 DB 节点
	nodeID := fmt.Sprintf("db_%d", time.Now().UnixNano())
	parentID := at.Stack.Top()
	depth := at.Stack.Depth()

	metadata := map[string]any{
		"operation":     operation,
		"table":         tableName,
		"duration_ms":   durationMs,
		"rows_affected": db.RowsAffected,
	}
	if sqlText != "" {
		metadata["sql"] = sqlText
	}

	metaJSON := ""
	if b, err := jsonMarshal(metadata); err == nil {
		metaJSON = string(b)
	}

	asyncInsertNode(&dao.TraceNode{
		TraceID:      at.TraceID,
		NodeID:       nodeID,
		ParentNodeID: parentID,
		Depth:        depth,
		NodeType:     NodeTypeDB,
		NodeName:     fmt.Sprintf("%s %s", operation, tableName),
		Status:       StatusSuccess,
		StartTime:    startTime,
		DurationMs:   durationMs,
		Metadata:     metaJSON,
	})
}

// jsonMarshal 辅助函数
func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}
