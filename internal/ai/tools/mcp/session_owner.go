package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"unicode/utf8"

	officialmcp "github.com/cloudwego/eino-ext/components/tool/mcp/officialmcp"
	officialsession "github.com/cloudwego/eino-ext/components/tool/mcp/officialmcp/session"
	"github.com/cloudwego/eino/components/tool"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ClientSession 是 officialmcp 所需的最小 SDK Session 能力。
type ClientSession interface {
	ListTools(context.Context, *sdkmcp.ListToolsParams) (*sdkmcp.ListToolsResult, error)
	CallTool(context.Context, *sdkmcp.CallToolParams) (*sdkmcp.CallToolResult, error)
}

// ErrServerDisabled 表示配置关闭了该 MCP Server。
var ErrServerDisabled = errors.New("mcp server is disabled")

type sessionConnector func(context.Context, ServerConfig) (ClientSession, func() error, error)

// SessionOwner 只持有官方 ClientSession 及其 Close；不实现协议或重连逻辑。
type SessionOwner struct {
	config  ServerConfig
	connect sessionConnector

	mu      sync.Mutex
	openMu  sync.Mutex
	session ClientSession
	close   func() error
	closed  bool
}

// NewSessionOwner 创建一个未打开的 Server 生命周期 owner。
func NewSessionOwner(config ServerConfig) (*SessionOwner, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &SessionOwner{config: config, connect: connectOfficialSession}, nil
}

// Open 建立初次 Session，并在 tools/list 失败时立即 Close；失败可按策略重试。
func (o *SessionOwner) Open(ctx context.Context) (ClientSession, error) {
	if o == nil {
		return nil, errors.New("mcp session owner is not configured")
	}
	if !o.config.Enabled {
		return nil, ErrServerDisabled
	}
	if o.connect == nil {
		return nil, errors.New("mcp session owner is not configured")
	}
	o.openMu.Lock()
	defer o.openMu.Unlock()
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return nil, errors.New("mcp session owner is closed")
	}
	if o.session != nil {
		session := o.session
		o.mu.Unlock()
		return session, nil
	}
	o.mu.Unlock()
	openCtx := ctx
	var cancel context.CancelFunc
	if o.config.Timeout > 0 {
		openCtx, cancel = context.WithTimeout(ctx, o.config.Timeout)
		defer cancel()
	}

	attempts, backoff := o.config.RetryPolicy()
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if err := openCtx.Err(); err != nil {
			return nil, err
		}
		session, closeFn, err := o.connect(openCtx, o.config)
		if err == nil {
			if _, listErr := session.ListTools(openCtx, nil); listErr != nil {
				_ = closeFn()
				lastErr = fmt.Errorf("mcp tools/list: %w", listErr)
			} else {
				o.mu.Lock()
				if o.closed {
					o.mu.Unlock()
					_ = closeFn()
					return nil, errors.New("mcp session owner is closed")
				}
				o.session, o.close = session, closeFn
				o.mu.Unlock()
				return session, nil
			}
		} else {
			lastErr = err
		}
		if attempt+1 < attempts {
			timer := time.NewTimer(backoff * time.Duration(1<<attempt))
			select {
			case <-openCtx.Done():
				timer.Stop()
				return nil, openCtx.Err()
			case <-timer.C:
			}
		}
	}
	if o.config.Required {
		return nil, fmt.Errorf("required mcp server %s unavailable: %w", o.config.Name, lastErr)
	}
	return nil, lastErr
}

// Tools returns officialmcp tools after a successful Open.
func (o *SessionOwner) Tools(ctx context.Context) ([]tool.BaseTool, error) {
	session, err := o.Open(ctx)
	if err != nil {
		return nil, err
	}
	if len(o.config.AllowedTools) == 0 {
		return []tool.BaseTool{}, nil
	}
	conf := &officialmcp.Config{
		Cli:               session,
		ServerName:        o.config.Name,
		ToolNameList:      o.config.ToolNameList(),
		ListToolsMode:     o.config.ListToolsMode,
		MaxToolPages:      o.config.MaxToolPages,
		ToolNameMapper:    StableToolNameMapper(o.config.EffectiveToolNamespace()),
		MetadataMode:      o.config.MetadataMode,
		DescriptionPolicy: &o.config.Description,
		ResultPolicy:      &o.config.Result,
		ToolCallResultHandlerV2: func(ctx context.Context, info officialmcp.ToolCallInfo, result *sdkmcp.CallToolResult) (*sdkmcp.CallToolResult, error) {
			if o.config.ObserveResult != nil {
				usage := ResultUsage{ServerName: info.ServerName, RawToolName: info.RawToolName, ExposedToolName: info.ExposedToolName}
				usage.Chars, usage.Bytes = resultSize(result)
				if result.IsError {
					usage.ErrorSummary = o.config.Redactor.RedactText(string(mustJSON(result.Content)))
				}
				o.config.ObserveResult(ctx, usage)
			}
			return result, nil
		},
	}
	tools, err := officialmcp.GetTools(ctx, conf)
	if err != nil {
		_ = o.Close()
		return nil, err
	}
	if o.config.Budget == nil && o.config.MaxResultBytes <= 0 && o.config.ToolTimeout <= 0 {
		return tools, nil
	}
	wrapped := make([]tool.BaseTool, 0, len(tools))
	for _, base := range tools {
		invokable, ok := base.(tool.InvokableTool)
		if !ok {
			wrapped = append(wrapped, base)
			continue
		}
		info, infoErr := base.Info(ctx)
		if infoErr != nil {
			return nil, infoErr
		}
		identity := o.config.ReservationIdentity
		if identity == nil {
			identity = func(ctx context.Context, name, args string) string {
				return defaultReservationIdentity(ctx, o.config.Name, name, args)
			}
		}
		wrapped = append(wrapped, &budgetTool{base: base, invokable: invokable, serverName: o.config.Name, toolName: info.Name, budget: o.config.Budget, identity: identity, maxChars: o.config.Result.MaxChars, maxBytes: o.config.MaxResultBytes, timeout: o.config.ToolTimeout})
	}
	return wrapped, nil
}

func mustJSON(value any) []byte {
	data, _ := json.Marshal(value)
	return data
}

// Close is idempotent and makes the owner unusable thereafter.
func (o *SessionOwner) Close() error {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return nil
	}
	o.closed = true
	closeFn := o.close
	o.session, o.close = nil, nil
	o.mu.Unlock()
	if closeFn == nil {
		return nil
	}
	return closeFn()
}

func connectOfficialSession(ctx context.Context, config ServerConfig) (ClientSession, func() error, error) {
	if config.Transport.Type == TransportStdio {
		cmd, err := buildStdioCommand(config.Transport)
		if err != nil {
			return nil, nil, err
		}
		transport := &sdkmcp.CommandTransport{Command: cmd}
		client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "sentinelops", Version: "v1"}, nil)
		session, err := client.Connect(ctx, transport, nil)
		if err != nil {
			return nil, nil, err
		}
		return session, session.Close, nil
	}
	transport := officialsession.TransportConfig{
		Type: config.Transport.Type, URL: config.Transport.URL, Command: config.Transport.Command,
		Args: append([]string(nil), config.Transport.Args...), Env: appendEnv(config.Transport.Env, config.Transport.EnvAllowlist), CWD: config.Transport.CWD,
		HTTPClient: NewSecureHTTPClient(EndpointPolicy{AllowedHosts: config.AllowedHosts, AllowedCIDRs: config.AllowedCIDRs, AllowedPorts: config.AllowedPorts, InsecureTLSForTest: config.AllowInsecureTLSForTest}),
	}
	if config.Transport.HeaderRef != "" {
		name := config.Transport.HeaderName
		if name == "" {
			name = "Authorization"
		}
		transport.HTTPClient = withSecretHeader(transport.HTTPClient, name, config.Transport.HeaderRef)
	}
	session, err := officialsession.Connect(ctx, officialsession.ServerConfig{
		Name:      config.Name,
		Transport: transport,
		Client:    &sdkmcp.Implementation{Name: "sentinelops", Version: "v1"},
	})
	if err != nil {
		return nil, nil, err
	}
	return session, session.Close, nil
}

func appendEnv(values map[string]string, allowlist []string) map[string]string {
	result := make(map[string]string, len(allowlist))
	for _, key := range allowlist {
		if value, ok := values[key]; ok {
			result[key] = value
		}
	}
	return result
}

func resultSize(result *sdkmcp.CallToolResult) (int64, int64) {
	if result == nil {
		return 0, 0
	}
	data, _ := json.Marshal(result)
	return int64(utf8.RuneCount(data)), int64(len(data))
}
