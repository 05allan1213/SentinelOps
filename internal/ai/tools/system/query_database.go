package system

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"SentinelOps/internal/ai/policy"
	dao "SentinelOps/internal/dao/mysql"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// QueryDatabaseInput SQL 只读查询参数
type QueryDatabaseInput struct {
	SQL string `json:"sql" jsonschema:"description=要执行的 SELECT 查询语句（仅支持只读查询，禁止 INSERT/UPDATE/DELETE/DROP 等写操作）"`
}

// QueryDatabaseGateCheck 在建立数据库查询前读取当前 admin debug Gate。
type QueryDatabaseGateCheck func(context.Context) (bool, error)

// NewQueryDatabaseTool 创建 query_database 工具。
// 复用 dao.DB 已有连接（无需 LLM 提供 DSN），仅允许 SELECT 查询，杜绝数据变更风险。
func NewQueryDatabaseTool(checks ...QueryDatabaseGateCheck) tool.InvokableTool {
	t, err := utils.InferOptionableTool(
		"query_database",
		"Execute a read-only SELECT query against the application database and return results as JSON. Use this to query raw data not covered by dedicated tools (e.g., aggregate statistics, joins across tables). Only SELECT statements are allowed — write operations must use dedicated tools.",
		func(ctx context.Context, input *QueryDatabaseInput, opts ...tool.Option) (output string, err error) {
			identity, err := policy.IdentityFromContext(ctx)
			if err != nil || identity.Role != policy.RoleAdmin {
				return "", policy.ErrForbidden
			}
			if len(checks) != 1 || checks[0] == nil {
				return "", policy.ErrForbidden
			}
			allowed, err := checks[0](ctx)
			if err != nil {
				return "", err
			}
			if !allowed {
				return "", policy.ErrForbidden
			}
			// 安全检查：只允许 SELECT 语句
			stmt := strings.TrimSpace(strings.ToUpper(input.SQL))
			if !strings.HasPrefix(stmt, "SELECT") {
				return "", fmt.Errorf("只允许执行 SELECT 查询，当前语句以 %q 开头", strings.Fields(stmt)[0])
			}

			db, err := dao.DB(ctx)
			if err != nil {
				return "", fmt.Errorf("database not available: %w", err)
			}

			// 使用 map slice 接收任意结构的查询结果
			var results []map[string]interface{}
			if err = db.Raw(input.SQL).Scan(&results).Error; err != nil {
				return "", fmt.Errorf("query failed: %w", err)
			}
			b, _ := json.Marshal(results)
			return string(b), nil
		})
	if err != nil {
		panic(err)
	}
	return t
}
