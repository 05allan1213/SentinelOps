// Package config 负责加载并校验 SentinelOps 应用配置。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcfg"
	"gopkg.in/yaml.v3"
)

const (
	baseFileName  = "config.yaml"
	localFileName = "config.local.yaml"

	// DriverOpenAICompatibleChat 使用 OpenAI-compatible Chat/Tool Calling 协议。
	DriverOpenAICompatibleChat = "openai_compatible_chat"
	// DriverOpenAICompatibleEmbedding 使用 OpenAI-compatible Embedding 协议。
	DriverOpenAICompatibleEmbedding = "openai_compatible_embedding"
	// DriverDashScopeCompatibleRerank 使用 DashScope-compatible Rerank 协议。
	DriverDashScopeCompatibleRerank = "dashscope_compatible_rerank"
)

// Config 是完整替代加载后的应用配置真值。
type Config struct {
	App              App                 `yaml:"app" json:"app"`
	Database         Database            `yaml:"database" json:"database"`
	Auth             Auth                `yaml:"auth" json:"auth"`
	SOAR             SOAR                `yaml:"soar" json:"soar"`
	Secrets          SecretReferences    `yaml:"secret_refs" json:"secret_refs"`
	AgentRuntime     AgentRuntime        `yaml:"agent_runtime" json:"agent_runtime"`
	ModelReliability ModelReliability    `yaml:"model_reliability" json:"model_reliability"`
	Providers        map[string]Provider `yaml:"providers" json:"providers"`
	ModelCatalog     map[string]Model    `yaml:"model_catalog" json:"model_catalog"`
	Routing          Routing             `yaml:"routing" json:"routing"`
	MCP              MCPConfig           `yaml:"mcp" json:"mcp"`
	Skill            SkillConfig         `yaml:"skill" json:"skill"`
	Observability    ObservabilityConfig `yaml:"observability" json:"observability"`
}

// ObservabilityConfig 保存可选官方导出与既有 Worker retention 的静态边界。
type ObservabilityConfig struct {
	Langfuse  LangfuseConfig  `yaml:"langfuse" json:"langfuse"`
	Retention RetentionConfig `yaml:"retention" json:"retention"`
}

// LangfuseConfig 不保存解析后的 Key；静态 Enabled 仍需数据库动态 Gate 同时允许。
type LangfuseConfig struct {
	Enabled                 bool      `yaml:"enabled" json:"enabled"`
	Host                    string    `yaml:"host" json:"host"`
	PublicKeyRef            SecretRef `yaml:"public_key_ref" json:"public_key_ref"`
	SecretKeyRef            SecretRef `yaml:"secret_key_ref" json:"secret_key_ref"`
	ServiceName             string    `yaml:"service_name" json:"service_name"`
	SampleRate              float64   `yaml:"sample_rate" json:"sample_rate"`
	TimeoutMS               int       `yaml:"timeout_ms" json:"timeout_ms"`
	MaxAttributeValueLength int       `yaml:"max_attribute_value_length" json:"max_attribute_value_length"`
	MaxSpanAttributeBytes   int       `yaml:"max_span_attribute_bytes" json:"max_span_attribute_bytes"`
	ShutdownTimeoutMS       int       `yaml:"shutdown_timeout_ms" json:"shutdown_timeout_ms"`
}

// RetentionConfig 配置同一 Worker poll loop 的有界清理批次；天数来自 admin 审计 settings。
type RetentionConfig struct {
	Enabled         bool `yaml:"enabled" json:"enabled"`
	LeaseDurationMS int  `yaml:"lease_duration_ms" json:"lease_duration_ms"`
	IntervalMS      int  `yaml:"interval_ms" json:"interval_ms"`
	BatchSize       int  `yaml:"batch_size" json:"batch_size"`
}

// MCPConfig 保存 MCP Server 的安全配置，不保存解析后的 Header Secret。
type MCPConfig struct {
	Enabled bool                 `yaml:"enabled" json:"enabled"`
	Servers map[string]MCPServer `yaml:"servers" json:"servers"`
}

// SkillConfig 保存只读 SOP Backend 的非敏感路径和大小上限。
type SkillConfig struct {
	Enabled  bool   `yaml:"enabled" json:"enabled"`
	BaseDir  string `yaml:"base_dir" json:"base_dir"`
	MaxBytes int64  `yaml:"max_bytes" json:"max_bytes"`
}

// MCPServer 是配置层的 MCP Server 描述；Transport/Policy 由 MCP 包消费。
type MCPServer struct {
	Enabled                  bool              `yaml:"enabled" json:"enabled"`
	Required                 bool              `yaml:"required" json:"required"`
	Transport                string            `yaml:"transport" json:"transport"`
	URL                      string            `yaml:"url" json:"url"`
	Command                  string            `yaml:"command" json:"command"`
	Args                     []string          `yaml:"args" json:"args"`
	Env                      map[string]string `yaml:"env" json:"env"`
	EnvAllowlist             []string          `yaml:"env_allowlist" json:"env_allowlist"`
	CWD                      string            `yaml:"cwd" json:"cwd"`
	HeaderName               string            `yaml:"header_name" json:"header_name"`
	HeaderRef                SecretRef         `yaml:"header_ref" json:"header_ref"`
	AllowedTools             []string          `yaml:"allowed_tools" json:"allowed_tools"`
	ToolNamespace            string            `yaml:"tool_namespace" json:"tool_namespace"`
	AllowedHosts             []string          `yaml:"allowed_hosts" json:"allowed_hosts"`
	AllowedCIDRs             []string          `yaml:"allowed_cidrs" json:"allowed_cidrs"`
	AllowedPorts             []int             `yaml:"allowed_ports" json:"allowed_ports"`
	MaxToolPages             int               `yaml:"max_tool_pages" json:"max_tool_pages"`
	MaxResultChars           int               `yaml:"max_result_chars" json:"max_result_chars"`
	MaxResultBytes           int               `yaml:"max_result_bytes" json:"max_result_bytes"`
	IncludeStructuredContent bool              `yaml:"include_structured_content" json:"include_structured_content"`
	IncludeMeta              bool              `yaml:"include_meta" json:"include_meta"`
	ErrorAsError             *bool             `yaml:"error_as_error" json:"error_as_error"`
	ListToolsMode            string            `yaml:"list_tools_mode" json:"list_tools_mode"`
	MetadataMode             string            `yaml:"metadata_mode" json:"metadata_mode"`
	DescriptionMaxChars      int               `yaml:"description_max_chars" json:"description_max_chars"`
	PreserveTailChars        int               `yaml:"preserve_tail_chars" json:"preserve_tail_chars"`
	ConnectAttempts          int               `yaml:"connect_attempts" json:"connect_attempts"`
	BackoffMS                int               `yaml:"backoff_ms" json:"backoff_ms"`
	TimeoutMS                int               `yaml:"timeout_ms" json:"timeout_ms"`
	ToolTimeoutMS            int               `yaml:"tool_timeout_ms" json:"tool_timeout_ms"`
}

// App 描述不含 Secret 的进程环境元数据。
type App struct {
	Environment string `yaml:"environment" json:"environment"`
}

// Database 保存数据库 Secret 引用，不保存完整 DSN。
type Database struct {
	MySQL MySQL `yaml:"mysql" json:"mysql"`
}

// MySQL 保存数据库连接 Secret 引用。
type MySQL struct {
	DSNRef    SecretRef `yaml:"dsn_ref" json:"dsn_ref"`
	LegacyDSN *string   `yaml:"dsn,omitempty" json:"-"`
}

// Auth 保存 JWT 与初始管理员 Secret 引用。
type Auth struct {
	JWT  JWT  `yaml:"jwt" json:"jwt"`
	Seed Seed `yaml:"seed" json:"seed"`
}

// JWT 描述 JWT 开关及签名 Secret 引用。
type JWT struct {
	Enabled      bool      `yaml:"enabled" json:"enabled"`
	SecretRef    SecretRef `yaml:"secret_ref" json:"secret_ref"`
	LegacySecret *string   `yaml:"secret,omitempty" json:"-"`
}

// Seed 保存初始管理员 Secret 引用。
type Seed struct {
	AdminPasswordRef    SecretRef `yaml:"admin_password_ref" json:"admin_password_ref"`
	LegacyAdminPassword *string   `yaml:"admin_password,omitempty" json:"-"`
}

// SecretReferences 为后续 MCP 与 Effect 调用预留同一 Resolver 的引用。
type SecretReferences struct {
	MCPHeader SecretRef `yaml:"mcp_header" json:"mcp_header"`
	Effect    SecretRef `yaml:"effect" json:"effect"`
}

// SOAR 保存现有集成配置，其中 SMTP 密码只能是引用。
type SOAR struct {
	Integrations Integrations `yaml:"integrations" json:"integrations"`
}

// Integrations 保存现有通知集成配置。
type Integrations struct {
	DingTalk DingTalk `yaml:"dingtalk" json:"dingtalk"`
	WeCom    WeCom    `yaml:"wecom" json:"wecom"`
	Email    Email    `yaml:"email" json:"email"`
}

// DingTalk 保存钉钉 endpoint Secret 引用。
type DingTalk struct {
	WebhookRef       SecretRef `yaml:"webhook_ref" json:"webhook_ref"`
	LegacyWebhookURL *string   `yaml:"webhook_url,omitempty" json:"-"`
}

// WeCom 保存企微 endpoint Secret 引用。
type WeCom struct {
	WebhookRef       SecretRef `yaml:"webhook_ref" json:"webhook_ref"`
	LegacyWebhookURL *string   `yaml:"webhook_url,omitempty" json:"-"`
}

// Email 保存 SMTP 元数据和密码引用。
type Email struct {
	SMTPHost           string    `yaml:"smtp_host" json:"smtp_host"`
	SMTPPort           string    `yaml:"smtp_port" json:"smtp_port"`
	SMTPUser           string    `yaml:"smtp_user" json:"smtp_user"`
	SMTPPasswordRef    SecretRef `yaml:"smtp_password_ref" json:"smtp_password_ref"`
	LegacySMTPPassword *string   `yaml:"smtp_pass,omitempty" json:"-"`
}

// AgentRuntime 保存 durable Agent 的静态 Gate；P42 前生产环境禁止 accept_new_runs。
type AgentRuntime struct {
	Enabled                 bool `yaml:"enabled" json:"enabled"`
	AcceptNewRuns           bool `yaml:"accept_new_runs" json:"accept_new_runs"`
	ShadowMode              bool `yaml:"shadow_mode" json:"shadow_mode"`
	AdminQueryDatabaseDebug bool `yaml:"admin_query_database_debug" json:"admin_query_database_debug"`
}

// ModelReliability 描述进程级 Retry、breaker 与模型 QPS 保护参数。
type ModelReliability struct {
	Retry   ModelRetry   `yaml:"retry" json:"retry"`
	Breaker ModelBreaker `yaml:"breaker" json:"breaker"`
	Limiter ModelLimiter `yaml:"limiter" json:"limiter"`
}

// ModelRetry 只配置 Eino 官方 Retry 的次数和退避基数。
type ModelRetry struct {
	MaxRetries    int `yaml:"max_retries" json:"max_retries"`
	BaseBackoffMS int `yaml:"base_backoff_ms" json:"base_backoff_ms"`
}

// ModelBreaker 配置按 provider-qualified Catalog Ref 隔离的跨请求健康状态。
type ModelBreaker struct {
	FailureThreshold int `yaml:"failure_threshold" json:"failure_threshold"`
	OpenTimeoutMS    int `yaml:"open_timeout_ms" json:"open_timeout_ms"`
}

// ModelLimiter 配置按 provider-qualified Catalog Ref 隔离的进程级限流。
type ModelLimiter struct {
	QPS   float64 `yaml:"qps" json:"qps"`
	Burst int     `yaml:"burst" json:"burst"`
}

// Provider 描述供应商实例的 Secret 引用与有限 Driver Endpoint。
type Provider struct {
	SecretRef    SecretRef         `yaml:"secret_ref" json:"secret_ref"`
	Endpoints    map[string]string `yaml:"endpoints" json:"endpoints"`
	LegacyAPIKey *string           `yaml:"api_key,omitempty" json:"-"`
}

type Model struct {
	ModelID      string   `yaml:"model_id" json:"model_id"`
	Driver       string   `yaml:"driver" json:"driver"`
	Capabilities []string `yaml:"capabilities" json:"capabilities"`
	Dimension    int      `yaml:"dimension,omitempty" json:"dimension,omitempty"`
	Pricing      Pricing  `yaml:"pricing" json:"pricing"`
}

type Pricing struct {
	Revision    string  `yaml:"revision" json:"revision"`
	Currency    string  `yaml:"currency" json:"currency"`
	Unit        string  `yaml:"unit" json:"unit"`
	Input       float64 `yaml:"input" json:"input"`
	CachedInput float64 `yaml:"cached_input,omitempty" json:"cached_input,omitempty"`
	Output      float64 `yaml:"output,omitempty" json:"output,omitempty"`
}

type Routing struct {
	Chat      map[string]ChatRoute `yaml:"chat" json:"chat"`
	Embedding map[string]Route     `yaml:"embedding" json:"embedding"`
	Rerank    map[string]Route     `yaml:"rerank" json:"rerank"`
}

// ChatRoute 是一个业务 Profile 的有序候选列表。
type ChatRoute struct {
	Candidates []Route `yaml:"candidates" json:"candidates"`
}

type Route struct {
	Model   string       `yaml:"model" json:"model"`
	Options RouteOptions `yaml:"options,omitempty" json:"options,omitempty"`
}

type RouteOptions struct {
	EnableThinking *bool  `yaml:"enable_thinking,omitempty" json:"enable_thinking,omitempty"`
	Instruct       string `yaml:"instruct,omitempty" json:"instruct,omitempty"`
}

var (
	currentMu sync.RWMutex
	current   *Config
)

// Parse 将一份完整 YAML 配置解析为结构体，不执行跨字段校验。
func Parse(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse configuration: %w", err)
	}
	return &cfg, nil
}

// LoadDirectory 优先加载完整的 config.local.yaml；本地配置不存在时才回退到
// config.yaml。两份配置不会合并，避免半覆盖导致实际生效值难以判断。
func LoadDirectory(dir string) (*Config, string, error) {
	selected := filepath.Join(dir, baseFileName)
	local := filepath.Join(dir, localFileName)
	if info, err := os.Stat(local); err == nil && !info.IsDir() {
		selected = local
	} else if err != nil && !os.IsNotExist(err) {
		return nil, "", fmt.Errorf("inspect local configuration: %w", err)
	}

	data, err := os.ReadFile(selected)
	if err != nil {
		return nil, "", fmt.Errorf("read configuration %s: %w", selected, err)
	}
	cfg, err := Parse(data)
	if err != nil {
		return nil, "", err
	}
	if err := cfg.Validate(); err != nil {
		return nil, "", fmt.Errorf("validate configuration %s: %w", selected, err)
	}

	adapter, err := gcfg.NewAdapterFile(filepath.Base(selected))
	if err != nil {
		return nil, "", fmt.Errorf("create GoFrame configuration adapter: %w", err)
	}
	if err := adapter.SetPath(filepath.Dir(selected)); err != nil {
		return nil, "", fmt.Errorf("set configuration directory: %w", err)
	}
	g.Cfg().SetAdapter(adapter)
	SetCurrent(cfg)
	return cfg, selected, nil
}

// SetCurrent 保存已经完成选择与校验的全局配置，供模型工厂显式读取。
func SetCurrent(cfg *Config) {
	currentMu.Lock()
	defer currentMu.Unlock()
	current = cfg
}

// Current 返回主程序加载后的全局配置；未加载时直接失败，禁止静默使用默认供应商。
func Current() (*Config, error) {
	currentMu.RLock()
	defer currentMu.RUnlock()
	if current == nil {
		return nil, fmt.Errorf("application configuration has not been loaded")
	}
	return current, nil
}

// Validate 校验 Provider、Model Catalog 和 Routing 之间的完整引用关系。
func (c *Config) Validate() error {
	environment := strings.TrimSpace(c.App.Environment)
	if environment == "" {
		return fmt.Errorf("app.environment is required")
	} else if environment != "development" && environment != "production" && environment != "test" {
		return fmt.Errorf("app.environment must be development, production, or test")
	}
	if c.Database.MySQL.LegacyDSN != nil {
		return fmt.Errorf("database.mysql.dsn plaintext is forbidden; use dsn_ref")
	}
	if c.Auth.JWT.LegacySecret != nil {
		return fmt.Errorf("auth.jwt.secret plaintext is forbidden; use secret_ref")
	}
	if c.Auth.Seed.LegacyAdminPassword != nil {
		return fmt.Errorf("auth.seed.admin_password plaintext is forbidden; use admin_password_ref")
	}
	if c.SOAR.Integrations.Email.LegacySMTPPassword != nil {
		return fmt.Errorf("soar.integrations.email.smtp_pass plaintext is forbidden; use smtp_password_ref")
	}
	if c.SOAR.Integrations.DingTalk.LegacyWebhookURL != nil {
		return fmt.Errorf("soar.integrations.dingtalk.webhook_url plaintext is forbidden; use webhook_ref")
	}
	if c.SOAR.Integrations.WeCom.LegacyWebhookURL != nil {
		return fmt.Errorf("soar.integrations.wecom.webhook_url plaintext is forbidden; use webhook_ref")
	}
	for name, ref := range map[string]SecretRef{
		"database.mysql.dsn_ref":                    c.Database.MySQL.DSNRef,
		"auth.jwt.secret_ref":                       c.Auth.JWT.SecretRef,
		"auth.seed.admin_password_ref":              c.Auth.Seed.AdminPasswordRef,
		"soar.integrations.email.smtp_password_ref": c.SOAR.Integrations.Email.SMTPPasswordRef,
		"soar.integrations.dingtalk.webhook_ref":    c.SOAR.Integrations.DingTalk.WebhookRef,
		"soar.integrations.wecom.webhook_ref":       c.SOAR.Integrations.WeCom.WebhookRef,
		"secret_refs.mcp_header":                    c.Secrets.MCPHeader,
		"secret_refs.effect":                        c.Secrets.Effect,
		"observability.langfuse.public_key_ref":     c.Observability.Langfuse.PublicKeyRef,
		"observability.langfuse.secret_key_ref":     c.Observability.Langfuse.SecretKeyRef,
	} {
		if ref != "" {
			if err := ref.Validate(); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
	}
	if c.Observability.Langfuse.Enabled {
		langfuse := c.Observability.Langfuse
		if strings.TrimSpace(langfuse.Host) == "" || strings.TrimSpace(langfuse.ServiceName) == "" {
			return fmt.Errorf("enabled Langfuse requires host and service_name")
		}
		if langfuse.PublicKeyRef == "" || langfuse.SecretKeyRef == "" {
			return fmt.Errorf("enabled Langfuse requires public_key_ref and secret_key_ref")
		}
		if langfuse.SampleRate < 0 || langfuse.SampleRate > 1 || langfuse.TimeoutMS <= 0 ||
			langfuse.MaxAttributeValueLength <= 0 || langfuse.MaxSpanAttributeBytes <= 0 || langfuse.ShutdownTimeoutMS <= 0 {
			return fmt.Errorf("enabled Langfuse has invalid sampling, timeout or attribute limits")
		}
	}
	if c.Observability.Retention.Enabled {
		retention := c.Observability.Retention
		if retention.LeaseDurationMS <= 0 || retention.IntervalMS <= 0 || retention.BatchSize <= 0 || retention.BatchSize > 1000 {
			return fmt.Errorf("enabled retention requires positive lease/interval and batch_size <= 1000")
		}
	}
	for name, server := range c.MCP.Servers {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("mcp server name is required")
		}
		if server.HeaderRef != "" {
			if err := server.HeaderRef.Validate(); err != nil {
				return fmt.Errorf("mcp server %s header_ref: %w", name, err)
			}
		}
		for key, value := range server.Env {
			if strings.TrimSpace(key) == "" || strings.TrimSpace(key) != key || strings.ContainsAny(key, "=\x00\r\n") || strings.ContainsAny(value, "\x00\r\n") {
				return fmt.Errorf("mcp server %s has invalid environment entry", name)
			}
		}
	}
	if len(c.Providers) == 0 {
		return fmt.Errorf("at least one provider is required")
	}
	for name, provider := range c.Providers {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("provider name is required")
		}
		if provider.LegacyAPIKey != nil {
			return fmt.Errorf("provider %s api_key plaintext is forbidden; use secret_ref", name)
		}
		if err := provider.SecretRef.Validate(); err != nil {
			return fmt.Errorf("provider %s secret_ref: %w", name, err)
		}
		for driver := range provider.Endpoints {
			switch driver {
			case DriverOpenAICompatibleChat, DriverOpenAICompatibleEmbedding, DriverDashScopeCompatibleRerank:
			default:
				return fmt.Errorf("provider %s uses unsupported endpoint driver %q", name, driver)
			}
		}
	}
	if c.ModelReliability.Retry.MaxRetries < 0 || c.ModelReliability.Retry.BaseBackoffMS <= 0 {
		return fmt.Errorf("model_reliability.retry requires non-negative max_retries and positive base_backoff_ms")
	}
	if c.ModelReliability.Breaker.FailureThreshold <= 0 || c.ModelReliability.Breaker.OpenTimeoutMS <= 0 {
		return fmt.Errorf("model_reliability.breaker requires positive failure_threshold and open_timeout_ms")
	}
	if c.ModelReliability.Limiter.QPS <= 0 || c.ModelReliability.Limiter.Burst <= 0 {
		return fmt.Errorf("model_reliability.limiter requires positive qps and burst")
	}
	if len(c.ModelCatalog) == 0 {
		return fmt.Errorf("model_catalog is required")
	}
	for ref, model := range c.ModelCatalog {
		providerName, err := providerFromRef(ref)
		if err != nil {
			return err
		}
		provider, ok := c.Providers[providerName]
		if !ok {
			return fmt.Errorf("model %s references missing provider %s", ref, providerName)
		}
		if model.ModelID == "" {
			return fmt.Errorf("model %s requires model_id", ref)
		}
		endpoint := strings.TrimSpace(provider.Endpoints[model.Driver])
		switch model.Driver {
		case DriverOpenAICompatibleChat:
			if endpoint == "" {
				return fmt.Errorf("model %s driver %s requires provider %s endpoint %s", ref, model.Driver, providerName, model.Driver)
			}
			if !hasCapability(model, "chat") || !hasCapability(model, "tool_calling") {
				return fmt.Errorf("model %s %s driver requires chat and tool_calling capabilities", ref, model.Driver)
			}
		case DriverOpenAICompatibleEmbedding:
			if endpoint == "" {
				return fmt.Errorf("model %s driver %s requires provider %s endpoint %s", ref, model.Driver, providerName, model.Driver)
			}
			if !hasCapability(model, "embedding") {
				return fmt.Errorf("model %s %s driver requires embedding capability", ref, model.Driver)
			}
			if model.Dimension != 2048 {
				return fmt.Errorf("model %s embedding dimension must be 2048", ref)
			}
		case DriverDashScopeCompatibleRerank:
			if endpoint == "" {
				return fmt.Errorf("model %s driver %s requires provider %s endpoint %s", ref, model.Driver, providerName, model.Driver)
			}
			if !hasCapability(model, "rerank") {
				return fmt.Errorf("model %s %s driver requires rerank capability", ref, model.Driver)
			}
		default:
			return fmt.Errorf("model %s uses unsupported driver %q", ref, model.Driver)
		}
		if strings.TrimSpace(model.Pricing.Revision) == "" {
			return fmt.Errorf("model %s pricing revision is required", ref)
		}
		if model.Pricing.Currency != "CNY" || model.Pricing.Unit != "per_million_tokens" {
			return fmt.Errorf("model %s pricing must use CNY per_million_tokens", ref)
		}
	}
	if err := c.validateChatRoute("routing.chat.default", c.Routing.Chat["default"]); err != nil {
		return err
	}
	if err := c.validateChatRoute("routing.chat.reasoning", c.Routing.Chat["reasoning"]); err != nil {
		return err
	}
	if err := c.validateRoute("routing.embedding.default", c.Routing.Embedding["default"], "embedding"); err != nil {
		return err
	}
	if err := c.validateRoute("routing.rerank.default", c.Routing.Rerank["default"], "rerank"); err != nil {
		return err
	}
	return nil
}

func (c *Config) validateChatRoute(name string, profile ChatRoute) error {
	if len(profile.Candidates) == 0 {
		return fmt.Errorf("%s requires at least one candidate", name)
	}
	seen := make(map[string]struct{}, len(profile.Candidates))
	for index, candidate := range profile.Candidates {
		candidateName := fmt.Sprintf("%s.candidates[%d]", name, index)
		if _, exists := seen[candidate.Model]; exists {
			return fmt.Errorf("%s duplicate candidate %s", name, candidate.Model)
		}
		seen[candidate.Model] = struct{}{}
		if err := c.validateRoute(candidateName, candidate, "chat", "tool_calling"); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) validateRoute(name string, route Route, capabilities ...string) error {
	if route.Model == "" {
		return fmt.Errorf("%s model is required", name)
	}
	model, ok := c.ModelCatalog[route.Model]
	if !ok {
		return fmt.Errorf("%s references missing model %s", name, route.Model)
	}
	for _, capability := range capabilities {
		if !hasCapability(model, capability) {
			return fmt.Errorf("%s model %s lacks %s capability", name, route.Model, capability)
		}
	}
	return nil
}

func providerFromRef(ref string) (string, error) {
	provider, model, ok := strings.Cut(ref, "/")
	if !ok || provider == "" || model == "" {
		return "", fmt.Errorf("model reference %q must use provider/model format", ref)
	}
	return provider, nil
}

func hasCapability(model Model, want string) bool {
	for _, capability := range model.Capabilities {
		if capability == want {
			return true
		}
	}
	return false
}

// Resolve 根据路由中的 provider/model 引用解析供应商和模型配置。
func (c *Config) Resolve(route Route) (Provider, Model, error) {
	// Provider 名称只取自 provider/model 引用，业务代码不依赖任何固定供应商。
	model, ok := c.ModelCatalog[route.Model]
	if !ok {
		return Provider{}, Model{}, fmt.Errorf("routing references missing model %s", route.Model)
	}
	providerName, err := providerFromRef(route.Model)
	if err != nil {
		return Provider{}, Model{}, err
	}
	provider, ok := c.Providers[providerName]
	if !ok {
		return Provider{}, Model{}, fmt.Errorf("routing references missing provider %s", providerName)
	}
	return provider, model, nil
}
