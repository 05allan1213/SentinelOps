package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"SentinelOps/internal/ai/policy"
	appconfig "SentinelOps/internal/config"

	officialmcp "github.com/cloudwego/eino-ext/components/tool/mcp/officialmcp"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// TransportSSE 是 MCP SSE Transport。
	TransportSSE = "sse"
	// TransportStdio 是 MCP stdio Transport。
	TransportStdio = "stdio"
	// TransportStreamableHTTP 是 MCP Streamable HTTP Transport。
	TransportStreamableHTTP = "streamable-http"

	defaultConnectAttempts = 3
	defaultBackoff         = 100 * time.Millisecond
)

// TransportConfig 描述一个 MCP Server 的官方 Transport 参数。
type TransportConfig struct {
	Type string
	URL  string

	Command      string
	Args         []string
	Env          map[string]string
	EnvAllowlist []string
	CWD          string

	HeaderName string
	HeaderRef  appconfig.SecretRef
}

// ServerConfig 是单个 MCP Server 的安全与 officialmcp Policy 配置。
type ServerConfig struct {
	Name     string
	Enabled  bool
	Required bool

	Transport TransportConfig

	AllowedTools  []string
	ToolNamespace string

	AllowedHosts []string
	AllowedCIDRs []string
	AllowedPorts []int

	ListToolsMode  officialmcp.ListToolsMode
	MaxToolPages   int
	MetadataMode   officialmcp.MetadataMode
	Description    officialmcp.DescriptionPolicy
	Result         officialmcp.ResultPolicy
	MaxResultBytes int

	ConnectAttempts int
	Backoff         time.Duration
	Timeout         time.Duration
	ToolTimeout     time.Duration

	AllowInsecureTLSForTest bool
	ObserveResult           ResultObserver
	Redactor                policy.Redactor
	Budget                  BudgetHook
	ReservationIdentity     func(context.Context, string, string) string
}

// Config 是 MCP Server 配置集合；Enabled=false 时不建立任何 Session。
type Config struct {
	Enabled bool
	Servers []ServerConfig
}

// FromAppConfig 将持久化配置转换为运行时 MCP policy，不在转换阶段解析 Secret。
func FromAppConfig(cfg *appconfig.Config) (Config, error) {
	if cfg == nil {
		return Config{}, errors.New("application config is required")
	}
	serverNames := make([]string, 0, len(cfg.MCP.Servers))
	for name := range cfg.MCP.Servers {
		serverNames = append(serverNames, name)
	}
	sort.Strings(serverNames)
	runtime := Config{Enabled: cfg.MCP.Enabled, Servers: make([]ServerConfig, 0, len(serverNames))}
	for _, name := range serverNames {
		stored := cfg.MCP.Servers[name]
		headerRef := stored.HeaderRef
		if headerRef == "" {
			headerRef = cfg.Secrets.MCPHeader
		}
		server := ServerConfig{
			Name: name, Enabled: stored.Enabled, Required: stored.Required,
			Transport:    TransportConfig{Type: stored.Transport, URL: stored.URL, Command: stored.Command, Args: append([]string(nil), stored.Args...), Env: cloneStringMap(stored.Env), EnvAllowlist: append([]string(nil), stored.EnvAllowlist...), CWD: stored.CWD, HeaderName: stored.HeaderName, HeaderRef: headerRef},
			AllowedTools: append([]string(nil), stored.AllowedTools...), ToolNamespace: stored.ToolNamespace, AllowedHosts: append([]string(nil), stored.AllowedHosts...), AllowedCIDRs: append([]string(nil), stored.AllowedCIDRs...), AllowedPorts: append([]int(nil), stored.AllowedPorts...),
			ListToolsMode: officialmcp.ListToolsMode(stored.ListToolsMode), MetadataMode: officialmcp.MetadataMode(stored.MetadataMode), MaxToolPages: stored.MaxToolPages, Description: officialmcp.DescriptionPolicy{MaxChars: stored.DescriptionMaxChars}, Result: officialmcp.ResultPolicy{MaxChars: stored.MaxResultChars, PreserveTailChars: stored.PreserveTailChars, IncludeStructuredContent: stored.IncludeStructuredContent, IncludeMeta: stored.IncludeMeta, ErrorAsError: cloneBool(stored.ErrorAsError)}, MaxResultBytes: stored.MaxResultBytes,
			ConnectAttempts: stored.ConnectAttempts, Backoff: time.Duration(stored.BackoffMS) * time.Millisecond, Timeout: time.Duration(stored.TimeoutMS) * time.Millisecond, ToolTimeout: time.Duration(stored.ToolTimeoutMS) * time.Millisecond,
		}
		if server.ToolNamespace == "" {
			server.ToolNamespace = name
		}
		if stored.Enabled {
			if err := server.Validate(); err != nil {
				return Config{}, fmt.Errorf("mcp server %s: %w", name, err)
			}
		}
		runtime.Servers = append(runtime.Servers, server)
	}
	return runtime, nil
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

// Validate 校验不含解析后 Secret 的 MCP 配置。
func (c ServerConfig) Validate() error {
	if strings.TrimSpace(c.Name) == "" {
		return errors.New("mcp server name is required")
	}
	if err := validateTransport(c.Transport); err != nil {
		return err
	}
	if c.Transport.HeaderRef != "" {
		if err := c.Transport.HeaderRef.Validate(); err != nil {
			return fmt.Errorf("mcp header_ref: %w", err)
		}
		if strings.ContainsAny(c.Transport.HeaderName, "\r\n") {
			return errors.New("mcp header name contains newline")
		}
	}
	seen := make(map[string]struct{}, len(c.AllowedTools))
	for _, name := range c.AllowedTools {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			return errors.New("mcp allowed tool name must not be empty")
		}
		if trimmed != name {
			return fmt.Errorf("mcp allowed tool %q must not contain surrounding whitespace", name)
		}
		if _, ok := seen[trimmed]; ok {
			return fmt.Errorf("mcp allowed tool %q is duplicated", trimmed)
		}
		seen[trimmed] = struct{}{}
	}
	if normalizeNamespace(c.EffectiveToolNamespace()) == "" {
		return errors.New("mcp tool namespace must contain an alphanumeric character")
	}
	if c.ListToolsMode != "" && c.ListToolsMode != officialmcp.ListToolsSinglePage && c.ListToolsMode != officialmcp.ListToolsAllPages {
		return fmt.Errorf("unsupported mcp list_tools_mode %q", c.ListToolsMode)
	}
	if c.MaxToolPages < 0 || c.MaxResultBytes < 0 || c.Description.MaxChars < 0 || c.Result.MaxChars < 0 || c.Result.PreserveTailChars < 0 {
		return errors.New("mcp policy limits must not be negative")
	}
	if c.ConnectAttempts < 0 || c.Backoff < 0 || c.Timeout < 0 || c.ToolTimeout < 0 {
		return errors.New("mcp lifecycle limits must not be negative")
	}
	policy := EndpointPolicy{AllowedHosts: c.AllowedHosts, AllowedCIDRs: c.AllowedCIDRs, AllowedPorts: c.AllowedPorts, InsecureTLSForTest: c.AllowInsecureTLSForTest}
	if c.Transport.Type != TransportStdio {
		if err := ValidateEndpoint(context.Background(), c.Transport.URL, policy); err != nil {
			return fmt.Errorf("mcp endpoint: %w", err)
		}
	} else if err := ValidateStdio(c.Transport); err != nil {
		return err
	}
	return nil
}

func validateTransport(transport TransportConfig) error {
	switch transport.Type {
	case TransportSSE, TransportStreamableHTTP:
		if strings.TrimSpace(transport.URL) == "" {
			return errors.New("mcp URL is required for HTTP transport")
		}
	case TransportStdio:
		return nil
	default:
		return fmt.Errorf("unsupported mcp transport %q", transport.Type)
	}
	return nil
}

// AllowsTool 实现 fail-closed 的 ToolNameList 语义；空列表拒绝全部。
func (c ServerConfig) AllowsTool(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || len(c.AllowedTools) == 0 {
		return false
	}
	for _, allowed := range c.AllowedTools {
		if name == allowed {
			return true
		}
	}
	return false
}

// ToolNameList 返回副本，保证调用方不能修改冻结配置。
func (c ServerConfig) ToolNameList() []string {
	return append([]string(nil), c.AllowedTools...)
}

// EffectiveToolNamespace returns the stable namespace used by the official mapper.
// An omitted namespace is scoped to the Server name rather than a process-wide default.
func (c ServerConfig) EffectiveToolNamespace() string {
	if strings.TrimSpace(c.ToolNamespace) == "" {
		return c.Name
	}
	return c.ToolNamespace
}

// RetryPolicy 返回显式的初次连接重试参数；不使用 sync.Once。
func (c ServerConfig) RetryPolicy() (int, time.Duration) {
	attempts := c.ConnectAttempts
	if attempts == 0 {
		attempts = defaultConnectAttempts
	}
	backoff := c.Backoff
	if backoff == 0 {
		backoff = defaultBackoff
	}
	return attempts, backoff
}

// StableToolNameMapper 生成仅依赖 Server namespace 与原始名称的官方 mapper。
func StableToolNameMapper(namespace string) officialmcp.ToolNameMapper {
	namespace = normalizeNamespace(namespace)
	return func(_ context.Context, info officialmcp.ToolNameMapperInput) (officialmcp.ToolNameMapperOutput, error) {
		if strings.TrimSpace(info.Tool.Name) == "" {
			return officialmcp.ToolNameMapperOutput{}, errors.New("mcp tool name is empty")
		}
		return officialmcp.ToolNameMapperOutput{ExposedName: namespace + "__" + info.Tool.Name}, nil
	}
}

func normalizeNamespace(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "mcp"
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

// ValidateToolNameConflicts 在跨 Server 合并工具前拒绝公开名称冲突。
func ValidateToolNameConflicts(names []string) error {
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if _, ok := seen[name]; ok {
			return fmt.Errorf("duplicate mcp exposed tool name %q", name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

// MCPToolSchemaHash 对 Server identity、原始/公开名称、Schema、annotations 和 description 做精确 Hash。
func MCPToolSchemaHash(serverName, exposedName string, tool sdkmcp.Tool) (string, error) {
	if strings.TrimSpace(serverName) == "" || strings.TrimSpace(exposedName) == "" || strings.TrimSpace(tool.Name) == "" {
		return "", errors.New("mcp schema hash identity is required")
	}
	value := struct {
		ServerName   string `json:"server_name"`
		RawName      string `json:"raw_tool_name"`
		ExposedName  string `json:"exposed_tool_name"`
		Description  string `json:"description"`
		Annotations  any    `json:"annotations,omitempty"`
		InputSchema  any    `json:"input_schema"`
		OutputSchema any    `json:"output_schema,omitempty"`
		Title        string `json:"title,omitempty"`
	}{serverName, tool.Name, exposedName, tool.Description, tool.Annotations, tool.InputSchema, tool.OutputSchema, tool.Title}
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal mcp schema: %w", err)
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

// MCPToolCatalogHash 聚合排序后的每个 mcp_tool_schema_hash，供 Runtime Snapshot 使用。
func MCPToolCatalogHash(serverName string, tools []sdkmcp.Tool, mapper officialmcp.ToolNameMapper) (string, error) {
	entries := make([]string, 0, len(tools))
	exposedNames := make([]string, 0, len(tools))
	for _, tool := range tools {
		exposed := tool.Name
		if mapper != nil {
			mapped, err := mapper(context.Background(), officialmcp.ToolNameMapperInput{ServerName: serverName, Tool: tool})
			if err != nil {
				return "", err
			}
			exposed = mapped.ExposedName
		}
		exposedNames = append(exposedNames, exposed)
	}
	if err := ValidateToolNameConflicts(exposedNames); err != nil {
		return "", err
	}
	for i, tool := range tools {
		exposed := exposedNames[i]
		hash, err := MCPToolSchemaHash(serverName, exposed, tool)
		if err != nil {
			return "", err
		}
		entries = append(entries, exposed+":"+hash)
	}
	sort.Strings(entries)
	hash := sha256.Sum256([]byte(strings.Join(entries, "\x00")))
	return hex.EncodeToString(hash[:]), nil
}

// ConfigCatalogHash 为尚未建立连接的启动快照提供确定性配置身份；解析后 Secret 和环境值不参与 Hash。
func ConfigCatalogHash(config Config) (string, error) {
	type identity struct {
		Name                     string                    `json:"name"`
		Enabled                  bool                      `json:"enabled"`
		Required                 bool                      `json:"required"`
		TransportType            string                    `json:"transport_type"`
		URL                      string                    `json:"url,omitempty"`
		Command                  string                    `json:"command,omitempty"`
		Args                     []string                  `json:"args,omitempty"`
		EnvAllowlist             []string                  `json:"env_allowlist,omitempty"`
		EnvKeys                  []string                  `json:"env_keys,omitempty"`
		CWD                      string                    `json:"cwd,omitempty"`
		HeaderName               string                    `json:"header_name,omitempty"`
		HeaderRef                appconfig.SecretRef       `json:"header_ref,omitempty"`
		AllowedTools             []string                  `json:"allowed_tools"`
		ToolNamespace            string                    `json:"tool_namespace"`
		AllowedHosts             []string                  `json:"allowed_hosts"`
		AllowedCIDRs             []string                  `json:"allowed_cidrs"`
		AllowedPorts             []int                     `json:"allowed_ports"`
		ListToolsMode            officialmcp.ListToolsMode `json:"list_tools_mode,omitempty"`
		MaxToolPages             int                       `json:"max_tool_pages,omitempty"`
		MetadataMode             officialmcp.MetadataMode  `json:"metadata_mode,omitempty"`
		DescriptionMaxChars      int                       `json:"description_max_chars,omitempty"`
		MaxResultChars           int                       `json:"max_result_chars,omitempty"`
		PreserveTailChars        int                       `json:"preserve_tail_chars,omitempty"`
		MaxResultBytes           int                       `json:"max_result_bytes,omitempty"`
		IncludeStructuredContent bool                      `json:"include_structured_content"`
		IncludeMeta              bool                      `json:"include_meta"`
		ErrorAsError             *bool                     `json:"error_as_error,omitempty"`
		ConnectAttempts          int                       `json:"connect_attempts,omitempty"`
		Backoff                  time.Duration             `json:"backoff,omitempty"`
		Timeout                  time.Duration             `json:"timeout,omitempty"`
		ToolTimeout              time.Duration             `json:"tool_timeout,omitempty"`
	}
	entries := make([]identity, 0, len(config.Servers))
	for _, server := range config.Servers {
		envKeys := make([]string, 0, len(server.Transport.Env))
		for key := range server.Transport.Env {
			envKeys = append(envKeys, key)
		}
		sort.Strings(envKeys)
		entries = append(entries, identity{Name: server.Name, Enabled: server.Enabled, Required: server.Required, TransportType: server.Transport.Type, URL: server.Transport.URL, Command: server.Transport.Command, Args: append([]string(nil), server.Transport.Args...), EnvAllowlist: append([]string(nil), server.Transport.EnvAllowlist...), EnvKeys: envKeys, CWD: server.Transport.CWD, HeaderName: server.Transport.HeaderName, HeaderRef: server.Transport.HeaderRef, AllowedTools: append([]string(nil), server.AllowedTools...), ToolNamespace: normalizeNamespace(server.EffectiveToolNamespace()), AllowedHosts: append([]string(nil), server.AllowedHosts...), AllowedCIDRs: append([]string(nil), server.AllowedCIDRs...), AllowedPorts: append([]int(nil), server.AllowedPorts...), ListToolsMode: server.ListToolsMode, MaxToolPages: server.MaxToolPages, MetadataMode: server.MetadataMode, DescriptionMaxChars: server.Description.MaxChars, MaxResultChars: server.Result.MaxChars, PreserveTailChars: server.Result.PreserveTailChars, MaxResultBytes: server.MaxResultBytes, IncludeStructuredContent: server.Result.IncludeStructuredContent, IncludeMeta: server.Result.IncludeMeta, ErrorAsError: cloneBool(server.Result.ErrorAsError), ConnectAttempts: server.ConnectAttempts, Backoff: server.Backoff, Timeout: server.Timeout, ToolTimeout: server.ToolTimeout})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	data, err := json.Marshal(struct {
		Enabled bool       `json:"enabled"`
		Servers []identity `json:"servers"`
	}{config.Enabled, entries})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

// ResultUsage 是单次 MCP 结果的非内容计量事实。
type ResultUsage struct {
	ServerName      string
	RawToolName     string
	ExposedToolName string
	Chars           int64
	Bytes           int64
	ErrorSummary    string
}

// ResultObserver 只接收计量事实，不接收或持久化 Secret。
type ResultObserver func(context.Context, ResultUsage)

// BudgetRequest 描述接入 P28 的 MCP reservation；不包含第二套 Store 或计数器。
type BudgetRequest struct {
	ReservationIdentity string
	Subject             string
	Calls               int64
	Concurrency         int64
	ResultChars         int64
	ResultBytes         int64
}

// BudgetSettlement 描述同一 reservation 的结算事实。
type BudgetSettlement struct {
	ReservationIdentity string
	Succeeded           bool
	ResultChars         int64
	ResultBytes         int64
}

// BudgetHook 是对 P28 同一 reservation primitive 的最小回调边界。
type BudgetHook interface {
	ReserveMCP(context.Context, BudgetRequest) error
	SettleMCP(context.Context, BudgetSettlement) error
}
