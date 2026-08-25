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
	App          App                 `yaml:"app" json:"app"`
	Database     Database            `yaml:"database" json:"database"`
	Auth         Auth                `yaml:"auth" json:"auth"`
	SOAR         SOAR                `yaml:"soar" json:"soar"`
	Secrets      SecretReferences    `yaml:"secret_refs" json:"secret_refs"`
	AgentRuntime AgentRuntime        `yaml:"agent_runtime" json:"agent_runtime"`
	Providers    map[string]Provider `yaml:"providers" json:"providers"`
	ModelCatalog map[string]Model    `yaml:"model_catalog" json:"model_catalog"`
	Routing      Routing             `yaml:"routing" json:"routing"`
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
	Email Email `yaml:"email" json:"email"`
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
	Chat      map[string]Route `yaml:"chat" json:"chat"`
	Embedding map[string]Route `yaml:"embedding" json:"embedding"`
	Rerank    map[string]Route `yaml:"rerank" json:"rerank"`
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
	for name, ref := range map[string]SecretRef{
		"database.mysql.dsn_ref":                    c.Database.MySQL.DSNRef,
		"auth.jwt.secret_ref":                       c.Auth.JWT.SecretRef,
		"auth.seed.admin_password_ref":              c.Auth.Seed.AdminPasswordRef,
		"soar.integrations.email.smtp_password_ref": c.SOAR.Integrations.Email.SMTPPasswordRef,
		"secret_refs.mcp_header":                    c.Secrets.MCPHeader,
		"secret_refs.effect":                        c.Secrets.Effect,
	} {
		if ref != "" {
			if err := ref.Validate(); err != nil {
				return fmt.Errorf("%s: %w", name, err)
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
	if err := c.validateRoute("routing.chat.default", c.Routing.Chat["default"], "chat", "tool_calling"); err != nil {
		return err
	}
	if err := c.validateRoute("routing.chat.reasoning", c.Routing.Chat["reasoning"], "chat", "tool_calling"); err != nil {
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
