package mysql

import (
	"time"

	"gorm.io/gorm"
)

// Event 安全事件（Content 仅在内存中流转，不落 MySQL，向量化后存 Milvus）
type Event struct {
	ID         string         `gorm:"column:id;primaryKey;size:64"`
	Title      string         `gorm:"column:title;size:256;not null"`
	Content    string         `gorm:"-"`                                       // 仅内存，不写库；入库前传给 IndexDocumentsAsync
	EventType  string         `gorm:"column:event_type;size:32;index"`         // 事件来源：rss / github / web / manual
	DedupKey   string         `gorm:"column:dedup_key;size:64;index"`          // SHA256(title|source|content[:500])，用于去重
	Severity   string         `gorm:"column:severity;size:32;index"`           // 严重等级：critical / high / medium / low
	Source     string         `gorm:"column:source;size:128;index"`            // 订阅源名称 或 web_search
	Status     string         `gorm:"column:status;size:32;default:new;index"` // 事件状态：new / processing / resolved / ignored
	CVEID      string         `gorm:"column:cve_id;size:64;index"`             // CVE-YYYY-NNNNN，web 类情报去重更新依据
	RiskScore  float64        `gorm:"column:risk_score"`                       // 0-10，由 severity 映射，0 表示未评估
	Metadata   string         `gorm:"column:metadata;type:json"`               // 扩展字段，如 {"link":"...","pub_date":"..."}
	RawPayload string         `gorm:"column:raw_payload;type:text"`            // 原始告警 payload（webhook/CEF/LEEF 原文）
	IndexedAt  *time.Time     `gorm:"column:indexed_at;type:datetime"`         // nil = 未向量化，非 nil = 已写入 Milvus
	CreatedAt  time.Time      `gorm:"column:created_at;type:datetime;autoCreateTime"`
	UpdatedAt  time.Time      `gorm:"column:updated_at;type:datetime;autoUpdateTime"`
	DeletedAt  gorm.DeletedAt `gorm:"column:deleted_at;index"`
}

func (Event) TableName() string { return "events" }

// Setting 系统配置 key-value 持久化
type Setting struct {
	Key       string    `gorm:"column:key;primaryKey;size:128"`
	Value     string    `gorm:"column:value;type:text"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:datetime;autoUpdateTime"`
}

func (Setting) TableName() string { return "settings" }

// Subscription 订阅源（RSS / GitHub）
type Subscription struct {
	ID          string         `gorm:"column:id;primaryKey;size:64"`
	Name        string         `gorm:"column:name;size:128;not null"`
	URL         string         `gorm:"column:url;size:512;not null"`
	Type        string         `gorm:"column:type;size:32;index"` // 订阅类型：rss / github
	CronExpr    string         `gorm:"column:cron_expr;size:64"`  // 抓取间隔，空时使用全局默认
	Enabled     bool           `gorm:"column:enabled;default:true"`
	LastFetchAt *time.Time     `gorm:"column:last_fetch_at;type:datetime"`
	CreatedAt   time.Time      `gorm:"column:created_at;type:datetime;autoCreateTime"`
	UpdatedAt   time.Time      `gorm:"column:updated_at;type:datetime;autoUpdateTime"`
	DeletedAt   gorm.DeletedAt `gorm:"column:deleted_at;index"`
}

func (Subscription) TableName() string { return "subscriptions" }

type SubscriptionFetchLog struct {
	ID             uint64    `gorm:"primaryKey;autoIncrement"`
	SubscriptionID string    `gorm:"column:subscription_id;size:64;not null;index"`
	Status         string    `gorm:"column:status;size:32;not null"`
	FetchedCount   int       `gorm:"column:fetched_count;default:0"`
	NewCount       int       `gorm:"column:new_count;default:0"`
	DurationMs     int64     `gorm:"column:duration_ms;default:0"`
	ErrorMsg       string    `gorm:"column:error_msg;type:text"`
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (SubscriptionFetchLog) TableName() string { return "subscription_fetch_logs" }

// Report 安全分析报告
type Report struct {
	ID        string         `gorm:"column:id;primaryKey;size:64"`
	Title     string         `gorm:"column:title;size:256;not null"`
	Content   string         `gorm:"column:content;type:longtext"`
	Type      string         `gorm:"column:type;size:32;index"` // 报告周期：weekly / monthly / custom
	CreatedAt time.Time      `gorm:"column:created_at;type:datetime;autoCreateTime"`
	UpdatedAt time.Time      `gorm:"column:updated_at;type:datetime;autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;index"`
}

func (Report) TableName() string { return "reports" }

// User 系统用户
type User struct {
	ID        string         `gorm:"column:id;primaryKey;size:64"`
	Username  string         `gorm:"column:username;size:64;uniqueIndex;not null"`
	Password  string         `gorm:"column:password;size:256;not null"`
	Role      string         `gorm:"column:role;size:32;default:user"` // 用户角色：admin / user
	CreatedAt time.Time      `gorm:"column:created_at;type:datetime;autoCreateTime"`
	UpdatedAt time.Time      `gorm:"column:updated_at;type:datetime;autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;index"`
}

func (User) TableName() string { return "users" }

// QueryTermMapping RAG 检索前的查询术语归一化规则
type QueryTermMapping struct {
	ID         uint      `gorm:"column:id;primaryKey;autoIncrement"`
	SourceTerm string    `gorm:"column:source_term;size:128;uniqueIndex;not null"`
	TargetTerm string    `gorm:"column:target_term;size:256;not null"`
	Priority   int       `gorm:"column:priority;default:0;index"` // 数值越大优先级越高
	Enabled    bool      `gorm:"column:enabled;default:true;index"`
	CreatedAt  time.Time `gorm:"column:created_at;type:datetime;autoCreateTime"`
	UpdatedAt  time.Time `gorm:"column:updated_at;type:datetime;autoUpdateTime"`
}

func (QueryTermMapping) TableName() string { return "query_term_mappings" }

// TraceRun 全链路运行记录（1条/请求）
type TraceRun struct {
	ID                uint       `gorm:"primaryKey;autoIncrement"`
	TraceID           string     `gorm:"column:trace_id;size:36;uniqueIndex;not null"`
	TraceName         string     `gorm:"column:trace_name;size:200"`
	EntryPoint        string     `gorm:"column:entry_point;size:200"`
	SessionID         string     `gorm:"column:session_id;size:100;index"`
	MessageIndex      int        `gorm:"column:message_index;default:0"`
	QueryText         string     `gorm:"column:query_text;type:text"`
	Status            string     `gorm:"column:status;size:20;default:running"`
	ErrorMessage      string     `gorm:"column:error_message;size:1000"`
	ErrorCode         string     `gorm:"column:error_code;size:50"`
	StartTime         time.Time  `gorm:"column:start_time;type:datetime(3)"`
	EndTime           *time.Time `gorm:"column:end_time;type:datetime(3)"`
	DurationMs        int64      `gorm:"column:duration_ms"`
	TotalInputTokens  int        `gorm:"column:total_input_tokens;default:0"`
	CachedInputTokens int        `gorm:"column:cached_input_tokens;default:0"`
	TotalOutputTokens int        `gorm:"column:total_output_tokens;default:0"`
	ReasoningTokens   int        `gorm:"column:reasoning_tokens;default:0"`
	EstimatedCostCNY  float64    `gorm:"column:estimated_cost_cny;type:decimal(10,6)"`
	Tags              string     `gorm:"column:tags;type:text"`
	CreatedAt         time.Time  `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt         time.Time  `gorm:"column:updated_at;autoUpdateTime"`
}

func (TraceRun) TableName() string { return "agent_trace_runs" }

// TraceNode 链路节点记录（N条/请求）
type TraceNode struct {
	ID                uint       `gorm:"primaryKey;autoIncrement"`
	TraceID           string     `gorm:"column:trace_id;size:36;index;not null"`
	NodeID            string     `gorm:"column:node_id;size:36;uniqueIndex;not null"`
	ParentNodeID      string     `gorm:"column:parent_node_id;size:36;index"`
	Depth             int        `gorm:"column:depth;default:0"`
	NodeType          string     `gorm:"column:node_type;size:50"`
	NodeName          string     `gorm:"column:node_name;size:200"`
	Status            string     `gorm:"column:status;size:20;default:running"`
	ErrorMessage      string     `gorm:"column:error_message;size:1000"`
	ErrorCode         string     `gorm:"column:error_code;size:50"`
	ErrorType         string     `gorm:"column:error_type;size:100"`
	StartTime         time.Time  `gorm:"column:start_time;type:datetime(3)"`
	EndTime           *time.Time `gorm:"column:end_time;type:datetime(3)"`
	DurationMs        int64      `gorm:"column:duration_ms"`
	ModelName         string     `gorm:"column:model_name;size:100"`
	InputTokens       int        `gorm:"column:input_tokens"`
	CachedInputTokens int        `gorm:"column:cached_input_tokens"`
	OutputTokens      int        `gorm:"column:output_tokens"`
	ReasoningTokens   int        `gorm:"column:reasoning_tokens"`
	CostCNY           float64    `gorm:"column:cost_cny;type:decimal(10,6)"`
	PromptText        string     `gorm:"column:prompt_text;type:longtext"`
	CompletionText    string     `gorm:"column:completion_text;type:text"`
	QueryText         string     `gorm:"column:query_text;type:text"`
	RetrievedDocs     string     `gorm:"column:retrieved_docs;type:text"`
	FinalTopK         int        `gorm:"column:final_top_k"`
	CacheHit          bool       `gorm:"column:cache_hit"`
	// 检索质量指标（仅 RetrievalNode / MilvusRetriever 节点写入）
	AvgVectorScore float64        `gorm:"column:avg_vector_score;type:decimal(6,4);default:0"`
	MaxVectorScore float64        `gorm:"column:max_vector_score;type:decimal(6,4);default:0"`
	DocCount       int            `gorm:"column:doc_count;default:0"`
	RerankUsed     bool           `gorm:"column:rerank_used;default:false"`
	AvgRerankScore float64        `gorm:"column:avg_rerank_score;type:decimal(6,4);default:0"`
	Metadata       string         `gorm:"column:metadata;type:text"`
	CreatedAt      time.Time      `gorm:"column:created_at;autoCreateTime"`
	DeletedAt      gorm.DeletedAt `gorm:"column:deleted_at;index"`
}

func (TraceNode) TableName() string { return "agent_trace_nodes" }

// KnowledgeBase 知识库（文档的逻辑分组）。
// 对应 Milvus 中 rag_store 集合的 documents 分区，所有文档向量共用一个分区，
// 通过 metadata.base_id 区分所属知识库（Milvus 内无二级分区）。
// ID 为 "default" 的记录是系统保留的默认知识库，上传的文件默认归入此库。
type KnowledgeBase struct {
	ID             string         `gorm:"column:id;primaryKey;size:64"`
	Name           string         `gorm:"column:name;size:128;not null"`
	Description    string         `gorm:"column:description;type:text"`
	DocCount       int            `gorm:"column:doc_count;default:0"`   // 文档总数（每次上传 +1、删除 -1）
	ChunkCount     int            `gorm:"column:chunk_count;default:0"` // 子块总数（索引完成时累加，删除文档时扣减）
	CreatedAt      time.Time      `gorm:"column:created_at;type:datetime;autoCreateTime"`
	UpdatedAt      time.Time      `gorm:"column:updated_at;type:datetime;autoUpdateTime"`
	DeletedAt      gorm.DeletedAt `gorm:"column:deleted_at;index"` // 软删除：删知识库时级联软删其下所有文档
	ContentHash    string         `gorm:"column:content_hash;size:64"`
	SourceVersion  string         `gorm:"column:source_version;size:128"`
	AccessScope    string         `gorm:"column:access_scope;size:191;not null;default:public"`
	IndexedVersion uint64         `gorm:"column:indexed_version;not null;default:0"`
	UpdatedBy      string         `gorm:"column:updated_by;size:128"`
}

func (KnowledgeBase) TableName() string { return "knowledge_bases" }

// KnowledgeDocument 已上传的知识文档。
// 每条记录对应 manifest/upload/knowledge/<base_id>/<doc_id>.<ext> 的本地文件。
// 文档状态流转：pending → indexing → completed / failed。
// 删除文档时同步清除 Milvus 向量、knowledge_chunks 记录和本地文件。
type KnowledgeDocument struct {
	ID              string         `gorm:"column:id;primaryKey;size:64"`
	BaseID          string         `gorm:"column:base_id;size:64;not null;index"`             // 所属知识库 ID
	Name            string         `gorm:"column:name;size:256;not null"`                     // 原始文件名（含扩展名）
	FilePath        string         `gorm:"column:file_path;size:512;not null"`                // 本地保存路径（绝对或相对 workdir）
	FileSize        int64          `gorm:"column:file_size;not null"`                         // 字节数，来自 os.Stat 或上传 multipart size
	FileType        string         `gorm:"column:file_type;size:32;not null;index"`           // 扩展名（小写，不含点），如 pdf / md / docx
	FileHash        string         `gorm:"column:file_hash;size:64;index"`                    // 文件 SHA256 hex（去重用），空值表示未计算
	ChunkStrategy   string         `gorm:"column:chunk_strategy;size:32;not null"`            // 分块策略：sliding_window / hierarchical / code
	ChunkConfig     string         `gorm:"column:chunk_config;type:json"`                     // ChunkConfig JSON 序列化（供重建索引时复用）
	ChunkCount      int            `gorm:"column:chunk_count;default:0"`                      // 索引完成后的实际子块数（indexing 阶段提前写入用于进度计算）
	IndexedChunks   int            `gorm:"column:indexed_chunks;default:0"`                   // 已写入 MySQL knowledge_chunks 的分块数（进度追踪，随批次递增）
	IndexedAt       *time.Time     `gorm:"column:indexed_at;type:datetime"`                   // nil = 未索引，非 nil = 最近一次索引完成时间
	IndexDurationMs int64          `gorm:"column:index_duration_ms;default:0"`                // 最近一次索引耗时（毫秒），0 = 未记录
	IndexStatus     string         `gorm:"column:index_status;size:32;default:pending;index"` // 索引状态：pending / indexing / completed / failed
	IndexError      string         `gorm:"column:index_error;type:text"`                      // 索引失败时的错误信息（completed 时清空）
	Enabled         bool           `gorm:"column:enabled;default:true"`                       // 是否启用，禁用时不参与 RAG 检索
	CreatedAt       time.Time      `gorm:"column:created_at;type:datetime;autoCreateTime"`
	UpdatedAt       time.Time      `gorm:"column:updated_at;type:datetime;autoUpdateTime"`
	DeletedAt       gorm.DeletedAt `gorm:"column:deleted_at;index"` // 软删除，硬数据（文件+向量）由 DeleteDoc 负责清理
	ContentHash     string         `gorm:"column:content_hash;size:64"`
	SourceVersion   string         `gorm:"column:source_version;size:128"`
	AccessScope     string         `gorm:"column:access_scope;size:191;not null;default:public"`
	IndexedVersion  uint64         `gorm:"column:indexed_version;not null;default:0"`
	UpdatedBy       string         `gorm:"column:updated_by;size:128"`
}

func (KnowledgeDocument) TableName() string { return "knowledge_documents" }

// KnowledgeChunk 文档分块的元数据记录（向量本体存在 Milvus documents 分区）。
// ChunkIndex 与 Milvus 文档 ID 一一对应，通过 metadata.doc_id + metadata.chunk_index 关联。
// ContentPreview 存储子块完整文本（type:text），供管理界面展示与关键词搜索；向量化使用的内容同步存储于 Milvus content 字段。
type KnowledgeChunk struct {
	ID             string    `gorm:"column:id;primaryKey;size:64"`         // 与 Milvus 文档 ID 相同（uuid）
	DocID          string    `gorm:"column:doc_id;size:64;not null;index"` // 所属文档 ID，删除文档时 WHERE doc_id = ? 批量清理
	ChunkIndex     int       `gorm:"column:chunk_index;not null"`          // 子块在文档中的全局序号（从 0 开始）
	ContentPreview string    `gorm:"column:content_preview;type:text"`     // 子块完整文本（供管理界面展示与关键词搜索）
	SectionTitle   string    `gorm:"column:section_title;size:256"`        // 章节标题（父子分块 / 结构化分块时填充，来自 ChunkResult.SectionTitle）
	CharCount      int       `gorm:"column:char_count;not null"`           // 子块字符数（rune 数，非字节数）
	Enabled        bool      `gorm:"column:enabled;default:true"`          // 是否启用，禁用时不参与 RAG 检索
	CreatedAt      time.Time `gorm:"column:created_at;type:datetime;autoCreateTime"`
	UpdatedAt      time.Time `gorm:"column:updated_at;type:datetime;autoUpdateTime"`
	ContentHash    string    `gorm:"column:content_hash;size:64"`
	SourceVersion  string    `gorm:"column:source_version;size:128"`
	AccessScope    string    `gorm:"column:access_scope;size:191;not null;default:public"`
	IndexedVersion uint64    `gorm:"column:indexed_version;not null;default:0"`
	UpdatedBy      string    `gorm:"column:updated_by;size:128"`
}

func (KnowledgeChunk) TableName() string { return "knowledge_chunks" }

// MessageFeedback 用户对 AI 回答的点赞/踩
type MessageFeedback struct {
	ID           uint      `gorm:"primaryKey;autoIncrement"`
	UserID       string    `gorm:"column:user_id;size:64;index"` // 关联用户，用于偏好推断
	SessionID    string    `gorm:"column:session_id;size:64;index"`
	MessageIndex int       `gorm:"column:message_index;not null"` // 消息在会话中的序号
	Vote         int       `gorm:"column:vote;not null"`          // 1=点赞，-1=点踩
	Reasons      string    `gorm:"column:reasons;type:json"`      // JSON 数组，如 ["too_verbose","inaccurate"]
	CreatedAt    time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (MessageFeedback) TableName() string { return "message_feedbacks" }

// UserPreference 用户偏好记忆（显式设置 + 隐式推断）
type UserPreference struct {
	ID            uint      `gorm:"primaryKey;autoIncrement"`
	UserID        string    `gorm:"column:user_id;size:64;uniqueIndex;not null"`
	OutputStyle   string    `gorm:"column:output_style;size:32;default:detailed"`   // 输出风格：detailed / concise
	AnalysisDepth string    `gorm:"column:analysis_depth;size:32;default:standard"` // 分析深度：quick / standard / deep
	FocusAreas    string    `gorm:"column:focus_areas;type:json"`                   // JSON 数组，如 ["web","supply_chain"]
	InferredNote  string    `gorm:"column:inferred_note;type:text"`                 // LLM 从对话中提取的偏好摘要
	UpdatedAt     time.Time `gorm:"column:updated_at;autoUpdateTime"`
	CreatedAt     time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (UserPreference) TableName() string { return "user_preferences" }

// ========== AI 智能运维模块 ==========

// OpsPlaybook 运维响应策略
type OpsPlaybook struct {
	ID          string         `gorm:"column:id;primaryKey;size:64"`
	Name        string         `gorm:"column:name;size:128;not null"`
	Description string         `gorm:"column:description;size:500"`
	Enabled     bool           `gorm:"column:enabled;default:true;index"`
	CreatedAt   time.Time      `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt   time.Time      `gorm:"column:updated_at;autoUpdateTime"`
	DeletedAt   gorm.DeletedAt `gorm:"column:deleted_at;index"`
}

func (OpsPlaybook) TableName() string { return "ops_playbooks" }

// OpsRun 运维任务执行记录
type OpsRun struct {
	ID         string     `gorm:"column:id;primaryKey;size:64"`
	PlaybookID string     `gorm:"column:playbook_id;size:64;not null;index"`
	EventID    string     `gorm:"column:event_id;size:64;index"`
	Status     string     `gorm:"column:status;size:32;default:running;index"`
	ErrorMsg   string     `gorm:"column:error_msg;type:text"`
	StartedAt  time.Time  `gorm:"column:started_at;type:datetime(3)"`
	FinishedAt *time.Time `gorm:"column:finished_at;type:datetime(3)"`
	DurationMs int64      `gorm:"column:duration_ms;default:0"`
	CreatedAt  time.Time  `gorm:"column:created_at;autoCreateTime"`
	// 非数据库字段
	PlanSummary   string `gorm:"-" json:"plan_summary,omitempty"`
	EventTitle    string `gorm:"-" json:"-"`
	EventSeverity string `gorm:"-" json:"-"`
}

func (OpsRun) TableName() string { return "ops_runs" }

// OpsRunStep 步骤执行明细
type OpsRunStep struct {
	ID             string     `gorm:"column:id;primaryKey;size:64"`
	RunID          string     `gorm:"column:run_id;size:64;not null;index"`
	StepID         string     `gorm:"column:step_id;size:64;not null"`
	StepOrder      int        `gorm:"column:step_order;not null"`
	ActionType     string     `gorm:"column:action_type;size:64"`
	ResolvedParams string     `gorm:"column:resolved_params;type:json"`
	Status         string     `gorm:"column:status;size:32;default:running"`
	Output         string     `gorm:"column:output;type:text"`
	ErrorMsg       string     `gorm:"column:error_msg;type:text"`
	RetryCount     int        `gorm:"column:retry_count;default:0"`
	StartedAt      time.Time  `gorm:"column:started_at;type:datetime(3)"`
	FinishedAt     *time.Time `gorm:"column:finished_at;type:datetime(3)"`
	DurationMs     int64      `gorm:"column:duration_ms;default:0"`
	CreatedAt      time.Time  `gorm:"column:created_at;autoCreateTime"`
}

func (OpsRunStep) TableName() string { return "ops_run_steps" }

// OpsProtectedAsset 受保护资产（禁止被自动封禁/操作）
type OpsProtectedAsset struct {
	ID        uint      `gorm:"primaryKey;autoIncrement"`
	AssetType string    `gorm:"column:asset_type;size:32;not null"`
	Value     string    `gorm:"column:value;size:256;not null"`
	Reason    string    `gorm:"column:reason;size:500"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (OpsProtectedAsset) TableName() string { return "ops_protected_assets" }

// WorkflowRun 工作流运行记录，保存一次运行的整体状态
type WorkflowRun struct {
	ID                       string         `gorm:"column:id;primaryKey;size:64"`
	WorkflowKey              string         `gorm:"column:workflow_key;size:128;not null;index"`
	UserID                   string         `gorm:"column:user_id;size:64;index"`
	SessionID                string         `gorm:"column:session_id;size:64;index"`
	ParentRunID              string         `gorm:"column:parent_run_id;size:64"`
	ActiveSessionKey         *string        `gorm:"column:active_session_key;size:64;uniqueIndex:uidx_workflow_runs_active_session"`
	Status                   string         `gorm:"column:status;size:32;default:running;index"`
	AvailableAt              time.Time      `gorm:"column:available_at;type:datetime(3);not null;default:CURRENT_TIMESTAMP(3)"`
	Priority                 int            `gorm:"column:priority;not null;default:0"`
	RuntimeMode              string         `gorm:"column:runtime_mode;size:32;not null;default:legacy"`
	Attempt                  uint           `gorm:"column:attempt;not null;default:0"`
	MaxAttempts              uint           `gorm:"column:max_attempts;not null;default:3"`
	LeaseOwner               *string        `gorm:"column:lease_owner;size:128"`
	LeaseUntil               *time.Time     `gorm:"column:lease_until;type:datetime(3)"`
	LeaseGeneration          uint64         `gorm:"column:lease_generation;not null;default:0"`
	HeartbeatAt              *time.Time     `gorm:"column:heartbeat_at;type:datetime(3)"`
	ImmutableInputJSON       *string        `gorm:"column:immutable_input_json;type:json"`
	QueryText                string         `gorm:"column:query_text;type:longtext"`
	ContextSnapshotJSON      *string        `gorm:"column:context_snapshot_json;type:json"`
	SessionRevision          *uint64        `gorm:"column:session_revision"`
	RuntimeVersion           *string        `gorm:"column:runtime_version;size:128"`
	RuntimeCompatibilityHash *string        `gorm:"column:runtime_compatibility_hash;type:char(64)"`
	AgentRevision            *string        `gorm:"column:agent_revision;size:128"`
	ModelSnapshot            *string        `gorm:"column:model_snapshot;type:json"`
	ToolSnapshot             *string        `gorm:"column:tool_snapshot;type:json"`
	MCPCatalogHash           *string        `gorm:"column:mcp_catalog_hash;type:char(64)"`
	SkillSnapshot            *string        `gorm:"column:skill_snapshot;type:json"`
	PromptHash               *string        `gorm:"column:prompt_hash;type:char(64)"`
	PolicyHash               *string        `gorm:"column:policy_hash;type:char(64)"`
	ConfigHash               *string        `gorm:"column:config_hash;type:char(64)"`
	FeatureSnapshot          *string        `gorm:"column:feature_snapshot;type:json"`
	BudgetLimitsJSON         *string        `gorm:"column:budget_limits_json;type:json"`
	BudgetUsageJSON          *string        `gorm:"column:budget_usage_json;type:json"`
	BudgetReservationsJSON   *string        `gorm:"column:budget_reservations_json;type:json"`
	UsageQuality             string         `gorm:"column:usage_quality;size:32;not null;default:unknown"`
	TraceQuality             string         `gorm:"column:trace_quality;size:32;not null;default:unknown"`
	LastEventSeq             uint64         `gorm:"column:last_event_seq;not null;default:0"`
	CancelRequestedAt        *time.Time     `gorm:"column:cancel_requested_at;type:datetime(3)"`
	ParkReason               *string        `gorm:"column:park_reason;size:128"`
	InputPayload             string         `gorm:"column:input_payload;type:text"`
	OutputPayload            string         `gorm:"column:output_payload;type:text"`
	ErrorMessage             string         `gorm:"column:error_message;type:text"`
	StartedAt                time.Time      `gorm:"column:started_at;type:datetime(3);not null"`
	FinishedAt               *time.Time     `gorm:"column:finished_at;type:datetime(3)"`
	DurationMs               int64          `gorm:"column:duration_ms;default:0"`
	CreatedAt                time.Time      `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt                time.Time      `gorm:"column:updated_at;autoUpdateTime"`
	DeletedAt                gorm.DeletedAt `gorm:"column:deleted_at;index"`
	// RuntimeAgent/RuntimeAgentQuality are read-only query projections, never persisted.
	RuntimeAgent        string `gorm:"-"`
	RuntimeAgentQuality string `gorm:"-"`
}

func (WorkflowRun) TableName() string { return "workflow_runs" }

// WorkflowEvent 工作流事件明细，按运行和序号保持唯一
type WorkflowEvent struct {
	ID                   uint      `gorm:"primaryKey;autoIncrement"`
	RunID                string    `gorm:"column:run_id;size:64;not null;uniqueIndex:idx_workflow_events_run_seq,priority:1"`
	Seq                  uint64    `gorm:"column:seq;not null;uniqueIndex:idx_workflow_events_run_seq,priority:2"`
	EventType            string    `gorm:"column:event_type;size:64;not null;index"`
	Payload              string    `gorm:"column:payload;type:text"`
	PayloadVersion       uint      `gorm:"column:payload_version;not null;default:1"`
	TraceID              string    `gorm:"column:trace_id;size:64"`
	OperationID          *string   `gorm:"column:operation_id;size:128"`
	CommandAction        *string   `gorm:"column:command_action;size:32"`
	IdempotencyKeyDigest *string   `gorm:"column:idempotency_key_digest;type:char(64)"`
	RequestFingerprint   *string   `gorm:"column:request_fingerprint;type:char(64)"`
	ActorID              *string   `gorm:"column:actor_id;size:128"`
	ReasonRedacted       *string   `gorm:"column:reason_redacted;type:text"`
	CorrelationSeq       *uint64   `gorm:"column:correlation_seq"`
	CreatedAt            time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (WorkflowEvent) TableName() string { return "workflow_events" }

// WorkflowAttempt 是 Durable Run 的查询投影；workflow_events 仍是事件真值。
// 可缺失的投影字段使用指针，以便查询层准确表达 partial/reconstructed。
type WorkflowAttempt struct {
	ID                          string     `gorm:"column:id;primaryKey;size:128"`
	RunID                       string     `gorm:"column:run_id;size:64;not null;uniqueIndex:uidx_workflow_attempts_run_attempt,priority:1"`
	Attempt                     uint       `gorm:"column:attempt;not null;uniqueIndex:uidx_workflow_attempts_run_attempt,priority:2"`
	Mode                        *string    `gorm:"column:mode;size:32"`
	Status                      *string    `gorm:"column:status;size:32"`
	CurrentPhase                *string    `gorm:"column:current_phase;size:32"`
	WorkerID                    *string    `gorm:"column:worker_id;size:128"`
	LeaseGeneration             *uint64    `gorm:"column:lease_generation"`
	RuntimeVersion              *string    `gorm:"column:runtime_version;size:128"`
	RunCompatibilityHash        *string    `gorm:"column:run_compatibility_hash;type:char(64)"`
	CheckpointCompatibilityHash *string    `gorm:"column:checkpoint_compatibility_hash;type:char(64)"`
	ExecutingWorkerFingerprint  *string    `gorm:"column:executing_worker_fingerprint;type:char(64)"`
	TraceID                     *string    `gorm:"column:trace_id;size:64"`
	OperationID                 *string    `gorm:"column:operation_id;size:128"`
	RetryCount                  *uint      `gorm:"column:retry_count"`
	FailoverCount               *uint      `gorm:"column:failover_count"`
	FailureCode                 *string    `gorm:"column:failure_code;size:64"`
	FailureMessageRedacted      *string    `gorm:"column:failure_message_redacted;type:text"`
	UsageQuality                *string    `gorm:"column:usage_quality;size:32"`
	TraceQuality                *string    `gorm:"column:trace_quality;size:32"`
	StartedAt                   *time.Time `gorm:"column:started_at;type:datetime(3)"`
	FinishedAt                  *time.Time `gorm:"column:finished_at;type:datetime(3)"`
	CreatedAt                   *time.Time `gorm:"column:created_at;type:datetime(3)"`
	UpdatedAt                   *time.Time `gorm:"column:updated_at;type:datetime(3)"`
}

func (WorkflowAttempt) TableName() string { return "workflow_attempts" }

// RuntimeWorkerSnapshot 保存 Worker 的脱敏运行时观测，不包含 Secret 或连接句柄。
type RuntimeWorkerSnapshot struct {
	WorkerID                 string     `gorm:"column:worker_id;primaryKey;size:128"`
	HeartbeatAt              *time.Time `gorm:"column:heartbeat_at;type:datetime(3)"`
	RuntimeVersion           *string    `gorm:"column:runtime_version;size:128"`
	RuntimeCompatibilityHash *string    `gorm:"column:runtime_compatibility_hash;type:char(64)"`
	ConfiguredCatalogHash    *string    `gorm:"column:configured_catalog_hash;type:char(64)"`
	ObservedMCPJSON          *string    `gorm:"column:observed_mcp_json;type:json"`
	ObservedSkillJSON        *string    `gorm:"column:observed_skill_json;type:json"`
	ActiveRunID              *string    `gorm:"column:active_run_id;size:64"`
	ActiveGeneration         *uint64    `gorm:"column:active_generation"`
	Status                   *string    `gorm:"column:status;size:32"`
	LastErrorRedacted        *string    `gorm:"column:last_error_redacted;type:text"`
	CreatedAt                *time.Time `gorm:"column:created_at;type:datetime(3)"`
	UpdatedAt                *time.Time `gorm:"column:updated_at;type:datetime(3)"`
}

func (RuntimeWorkerSnapshot) TableName() string { return "runtime_worker_snapshots" }

// WorkflowCheckpoint 工作流检查点，隔离 legacy JSON 与 Eino opaque bytes。
type WorkflowCheckpoint struct {
	ID                       string         `gorm:"column:id;primaryKey;size:64"`
	RunID                    string         `gorm:"column:run_id;size:64;not null;index"`
	CheckpointKey            string         `gorm:"column:checkpoint_key;size:128;not null;index"`
	SnapshotJSON             string         `gorm:"column:snapshot_json;type:json;not null"`
	EinoCheckpointID         *string        `gorm:"column:eino_checkpoint_id;size:128;uniqueIndex:uidx_workflow_checkpoints_eino_id"`
	CheckpointBlob           []byte         `gorm:"column:checkpoint_blob;type:longblob"`
	PayloadSHA256            *string        `gorm:"column:payload_sha256;type:char(64)"`
	RuntimeVersion           *string        `gorm:"column:runtime_version;size:128"`
	RuntimeCompatibilityHash *string        `gorm:"column:runtime_compatibility_hash;type:char(64)"`
	LeaseGeneration          *uint64        `gorm:"column:lease_generation"`
	CommittedAt              *time.Time     `gorm:"column:committed_at;type:datetime(3)"`
	ExpiresAt                *time.Time     `gorm:"column:expires_at;type:datetime(3)"`
	CreatedAt                time.Time      `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt                time.Time      `gorm:"column:updated_at;autoUpdateTime"`
	DeletedAt                gorm.DeletedAt `gorm:"column:deleted_at;index"`
	RuntimeAgent             string         `gorm:"-"`
	RuntimeAgentQuality      string         `gorm:"-"`
}

func (WorkflowCheckpoint) TableName() string { return "workflow_checkpoints" }

// AgentApproval 保存一次稳定 Proposal 的审批事实。
type AgentApproval struct {
	ID                        string     `gorm:"column:id;primaryKey;size:128"`
	RunID                     string     `gorm:"column:run_id;size:64;not null;uniqueIndex:uidx_agent_approvals_run_proposal,priority:1"`
	ToolCallIDObserved        *string    `gorm:"column:tool_call_id_observed;size:128"`
	ToolName                  string     `gorm:"column:tool_name;size:128;not null"`
	ToolRevision              string     `gorm:"column:tool_revision;size:128;not null"`
	ToolSchemaHash            string     `gorm:"column:tool_schema_hash;type:char(64);not null"`
	RiskLevel                 string     `gorm:"column:risk_level;size:32;not null"`
	ProposalJSONRedacted      string     `gorm:"column:proposal_json_redacted;type:json;not null"`
	ProposalHash              string     `gorm:"column:proposal_hash;type:char(64);not null;uniqueIndex:uidx_agent_approvals_run_proposal,priority:2"`
	PolicyHash                string     `gorm:"column:policy_hash;type:char(64);not null"`
	RuntimeCompatibilityHash  string     `gorm:"column:runtime_compatibility_hash;type:char(64);not null"`
	RequestedBy               string     `gorm:"column:requested_by;size:128;not null"`
	DecidedBy                 *string    `gorm:"column:decided_by;size:128"`
	Status                    string     `gorm:"column:status;size:32;not null;default:preparing"`
	Version                   uint64     `gorm:"column:version;not null;default:1"`
	DecisionReason            *string    `gorm:"column:decision_reason;type:text"`
	InterruptID               *string    `gorm:"column:interrupt_id;size:128"`
	InterruptAddress          *string    `gorm:"column:interrupt_address;size:512"`
	CheckpointID              *string    `gorm:"column:checkpoint_id;size:128"`
	CheckpointPayloadSHA256   *string    `gorm:"column:checkpoint_payload_sha256;type:char(64)"`
	CheckpointLeaseGeneration *uint64    `gorm:"column:checkpoint_lease_generation"`
	PreparingAt               time.Time  `gorm:"column:preparing_at;type:datetime(3);not null"`
	PublishedAt               *time.Time `gorm:"column:published_at;type:datetime(3)"`
	ExpiresAt                 *time.Time `gorm:"column:expires_at;type:datetime(3)"`
	CreatedAt                 time.Time  `gorm:"column:created_at;type:datetime(3);not null"`
	DecidedAt                 *time.Time `gorm:"column:decided_at;type:datetime(3)"`
}

func (AgentApproval) TableName() string { return "agent_approvals" }

// AgentEffect 保存稳定 Proposal step 的幂等执行事实。
type AgentEffect struct {
	ID                         string     `gorm:"column:id;primaryKey;size:128"`
	RunID                      string     `gorm:"column:run_id;size:64;not null;uniqueIndex:uidx_agent_effects_proposal_step,priority:1"`
	ToolCallIDObserved         *string    `gorm:"column:tool_call_id_observed;size:128"`
	IdempotencyKey             string     `gorm:"column:idempotency_key;size:191;not null;uniqueIndex:uidx_agent_effects_idempotency"`
	EffectRole                 string     `gorm:"column:effect_role;size:32;not null;default:primary"`
	EffectStep                 string     `gorm:"column:effect_step;size:128;not null;uniqueIndex:uidx_agent_effects_proposal_step,priority:3"`
	ParentEffectID             *string    `gorm:"column:parent_effect_id;size:128"`
	ProposalHash               string     `gorm:"column:proposal_hash;type:char(64);not null;uniqueIndex:uidx_agent_effects_proposal_step,priority:2"`
	ToolName                   string     `gorm:"column:tool_name;size:128;not null"`
	ToolRevision               string     `gorm:"column:tool_revision;size:128;not null"`
	ToolSchemaHash             string     `gorm:"column:tool_schema_hash;type:char(64);not null"`
	TargetHash                 string     `gorm:"column:target_hash;type:char(64);not null"`
	RequestRedacted            *string    `gorm:"column:request_redacted;type:json"`
	ResponseRedacted           *string    `gorm:"column:response_redacted;type:json"`
	EffectType                 string     `gorm:"column:effect_type;size:64;not null"`
	Status                     string     `gorm:"column:status;size:32;not null;default:pending"`
	Version                    uint64     `gorm:"column:version;not null;default:1"`
	ExternalReference          *string    `gorm:"column:external_reference;size:512"`
	LeaseGeneration            uint64     `gorm:"column:lease_generation;not null"`
	Attempt                    uint       `gorm:"column:attempt;not null;default:0"`
	ReconciliationAttempts     uint       `gorm:"column:reconciliation_attempts;not null;default:0"`
	NextReconcileAt            *time.Time `gorm:"column:next_reconcile_at;type:datetime(3)"`
	Resolution                 *string    `gorm:"column:resolution;size:64"`
	ResolutionEvidenceRedacted *string    `gorm:"column:resolution_evidence_redacted;type:json"`
	ResolvedBy                 *string    `gorm:"column:resolved_by;size:128"`
	ResolvedAt                 *time.Time `gorm:"column:resolved_at;type:datetime(3)"`
	StartedAt                  *time.Time `gorm:"column:started_at;type:datetime(3)"`
	FinishedAt                 *time.Time `gorm:"column:finished_at;type:datetime(3)"`
	LastError                  *string    `gorm:"column:last_error;type:text"`
	CreatedAt                  time.Time  `gorm:"column:created_at;type:datetime(3);not null"`
	UpdatedAt                  time.Time  `gorm:"column:updated_at;type:datetime(3);not null"`
}

func (AgentEffect) TableName() string { return "agent_effects" }

// SessionStateRevision 会话状态修订记录，保存会话状态的版本化快照
type SessionStateRevision struct {
	ID        uint           `gorm:"primaryKey;autoIncrement"`
	SessionID string         `gorm:"column:session_id;size:64;not null;uniqueIndex:idx_session_state_revisions_session_revision,priority:1"`
	Revision  uint64         `gorm:"column:revision;not null;uniqueIndex:idx_session_state_revisions_session_revision,priority:2"`
	StateJSON string         `gorm:"column:state_json;type:json;not null"`
	CreatedAt time.Time      `gorm:"column:created_at;autoCreateTime"`
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;index"`
}

func (SessionStateRevision) TableName() string { return "session_state_revisions" }
