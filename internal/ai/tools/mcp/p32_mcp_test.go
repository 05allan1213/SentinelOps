package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appconfig "SentinelOps/internal/config"

	officialmcp "github.com/cloudwego/eino-ext/components/tool/mcp/officialmcp"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPEmptyAllowedToolsRejectsAll(t *testing.T) {
	server := ServerConfig{
		Name:          "inventory",
		AllowedTools:  []string{},
		ToolNamespace: "inventory",
		Transport:     TransportConfig{Type: TransportStreamableHTTP, URL: "https://mcp.example.test"},
	}
	if server.AllowsTool("search") {
		t.Fatal("empty allowed_tools must reject every tool")
	}
}

func TestMCPAllowedToolsPreservesNames(t *testing.T) {
	server := ServerConfig{AllowedTools: []string{"search", "read"}}
	if !server.AllowsTool("search") || !server.AllowsTool("read") || server.AllowsTool("delete") {
		t.Fatal("non-empty allowed_tools must be an exact ToolNameList")
	}
	if err := (ServerConfig{Name: "inventory", Transport: TransportConfig{Type: TransportStdio, Command: "/bin/echo", CWD: "/tmp"}, AllowedTools: []string{" search"}}).Validate(); err == nil {
		t.Fatal("allowed tool names with surrounding whitespace must be rejected")
	}
}

func TestMCPConfigUsesSecretReferenceAndDeterministicServerOrder(t *testing.T) {
	errorAsError := true
	cfg, err := FromAppConfig(&appconfig.Config{
		Secrets: appconfig.SecretReferences{MCPHeader: "env:MCP_HEADER"},
		MCP: appconfig.MCPConfig{Enabled: true, Servers: map[string]appconfig.MCPServer{
			"zeta":  {Enabled: false},
			"alpha": {Enabled: false, HeaderRef: "env:SERVER_HEADER", AllowedTools: []string{"read"}, ErrorAsError: &errorAsError, ToolNamespace: "inventory", DescriptionMaxChars: 12, PreserveTailChars: 3},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 2 || cfg.Servers[0].Name != "alpha" || cfg.Servers[1].Name != "zeta" {
		t.Fatalf("server order = %#v", cfg.Servers)
	}
	if cfg.Servers[0].Transport.HeaderRef != "env:SERVER_HEADER" {
		t.Fatalf("server-specific SecretRef was not preserved: %q", cfg.Servers[0].Transport.HeaderRef)
	}
	if cfg.Servers[1].Transport.HeaderRef != "env:MCP_HEADER" {
		t.Fatalf("global MCP SecretRef fallback was not applied: %q", cfg.Servers[1].Transport.HeaderRef)
	}
	if cfg.Servers[0].Result.ErrorAsError == nil || !*cfg.Servers[0].Result.ErrorAsError {
		t.Fatal("result ErrorAsError policy was not copied")
	}
	if cfg.Servers[1].EffectiveToolNamespace() != "zeta" {
		t.Fatalf("empty namespace did not default to server name: %q", cfg.Servers[1].EffectiveToolNamespace())
	}
	hash1, err := ConfigCatalogHash(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Servers[0].AllowedTools[0] = "changed"
	hash2, err := ConfigCatalogHash(cfg)
	if err != nil || hash1 == hash2 {
		t.Fatalf("MCP config catalog hash did not change with exposed tool policy: %q %q", hash1, hash2)
	}
	cfg.Servers[0].AllowedTools[0] = "read"
	cfg.Servers[0].Description.MaxChars++
	hash3, err := ConfigCatalogHash(cfg)
	if err != nil || hash2 == hash3 {
		t.Fatalf("MCP config catalog hash did not change with description policy: %q %q", hash2, hash3)
	}
}

func TestMCPSSRFChecksDNSRedirectMetadataAndPrivateAllowlist(t *testing.T) {
	lookup := func(_ context.Context, host string) ([]net.IP, error) {
		switch host {
		case "public.example":
			return []net.IP{{203, 0, 113, 10}}, nil
		case "private.example":
			return []net.IP{{10, 0, 0, 7}}, nil
		case "metadata.example":
			return []net.IP{{169, 254, 169, 254}}, nil
		case "metadata-v6.example":
			return []net.IP{net.ParseIP("fd00:ec2::254")}, nil
		default:
			return nil, errors.New("unexpected host")
		}
	}
	policy := EndpointPolicy{LookupIP: lookup, AllowedHosts: []string{"private.example"}, AllowedPorts: []int{443}}
	if err := ValidateEndpoint(context.Background(), "https://public.example/path", policy); err != nil {
		t.Fatalf("public endpoint rejected: %v", err)
	}
	if err := ValidateEndpoint(context.Background(), "https://private.example/path", policy); err != nil {
		t.Fatalf("explicit private endpoint rejected: %v", err)
	}
	if err := ValidateEndpoint(context.Background(), "https://metadata.example/path", policy); err == nil {
		t.Fatal("metadata address must always be rejected")
	}
	if err := ValidateEndpoint(context.Background(), "https://metadata-v6.example/path", policy); err == nil {
		t.Fatal("IPv6 metadata address must always be rejected")
	}
	if err := ValidateEndpoint(context.Background(), "http://public.example/path", policy); err == nil {
		t.Fatal("non-HTTPS endpoint must be rejected")
	}
	if err := ValidateEndpoint(context.Background(), "https://user:pass@public.example/path", policy); err == nil {
		t.Fatal("endpoint userinfo must be rejected")
	}
	if err := ValidateEndpoint(context.Background(), "https://public.example:8443/path", policy); err == nil {
		t.Fatal("unlisted port must be rejected")
	}
}

type p32SecretResolver struct{ value []byte }

func (r *p32SecretResolver) Resolve(context.Context, appconfig.SecretRef) ([]byte, error) {
	return append([]byte(nil), r.value...), nil
}

func TestMCPSecretHeaderResolvesOnlyDuringRequest(t *testing.T) {
	old := appconfig.NewEnvironmentResolver()
	resolver := &p32SecretResolver{value: []byte("Bearer short-lived")}
	appconfig.SetSecretResolver(resolver)
	defer appconfig.SetSecretResolver(old)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer short-lived" {
			t.Errorf("authorization header = %q", got)
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer server.Close()
	client := withSecretHeader(server.Client(), "Authorization", appconfig.SecretRef("env:MCP_HEADER"))
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(response.Body)
	_ = response.Body.Close()
	if strings.Contains(string(resolver.value), "Bearer") {
		resolver.value = nil
	}
}

func TestMCPRedirectIsCheckedOnEveryHop(t *testing.T) {
	redirected := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !redirected {
			redirected = true
			http.Redirect(w, r, "http://169.254.169.254/latest", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	policy := EndpointPolicy{AllowedHosts: []string{"127.0.0.1"}, AllowedPorts: []int{server.Listener.Addr().(*net.TCPAddr).Port}, InsecureTLSForTest: true}
	client := NewSecureHTTPClient(policy)
	_, err := client.Get(server.URL)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "endpoint") {
		t.Fatalf("redirect SSRF was not rejected: %v", err)
	}
}

func TestMCPStdioRequiresAbsoluteCommandAndExplicitEnvironment(t *testing.T) {
	if err := ValidateStdio(TransportConfig{Type: TransportStdio, Command: "echo"}); err == nil {
		t.Fatal("relative stdio command must be rejected")
	}
	command := filepath.Join(t.TempDir(), "server")
	if err := os.WriteFile(command, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ValidateStdio(TransportConfig{
		Type:         TransportStdio,
		Command:      command,
		CWD:          t.TempDir(),
		Env:          map[string]string{"SAFE": "1"},
		EnvAllowlist: []string{"SAFE"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateStdio(TransportConfig{Type: TransportStdio, Command: command, CWD: t.TempDir(), EnvAllowlist: []string{"SAFE", "SAFE"}}); err == nil {
		t.Fatal("duplicate stdio environment allowlist names must be rejected")
	}
	cmd, err := buildStdioCommand(TransportConfig{Type: TransportStdio, Command: command, Args: []string{"--once"}, CWD: "/tmp", Env: map[string]string{"SAFE": "1"}, EnvAllowlist: []string{"SAFE"}})
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(cmd.Path) || strings.Contains(filepath.Base(cmd.Path), "sh") || strings.Contains(strings.Join(cmd.Args, " "), "-c") {
		t.Fatalf("stdio command unexpectedly uses a shell: %#v", cmd)
	}
	if strings.Join(cmd.Env, "\x00") != "SAFE=1" {
		t.Fatalf("stdio environment was not reduced to allowlist: %v", cmd.Env)
	}
}

func TestMCPSessionFailureClosesAndCanRetry(t *testing.T) {
	owner := &SessionOwner{config: ServerConfig{Name: "required", Enabled: true, Required: true}, connect: func(context.Context, ServerConfig) (ClientSession, func() error, error) {
		return nil, nil, errors.New("temporary startup failure")
	}}
	if _, err := owner.Open(context.Background()); err == nil {
		t.Fatal("required startup failure must be returned")
	}
	if owner.closed {
		t.Fatal("failed initial connect must remain retryable")
	}
}

type p32FakeSession struct {
	listErr error
	closeN  int
}

func (s *p32FakeSession) ListTools(context.Context, *mcp.ListToolsParams) (*mcp.ListToolsResult, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return &mcp.ListToolsResult{}, nil
}

func (*p32FakeSession) CallTool(context.Context, *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	return &mcp.CallToolResult{}, nil
}

func TestMCPOpenToolsListFailureClosesImmediately(t *testing.T) {
	fake := &p32FakeSession{listErr: errors.New("list unavailable")}
	closed := false
	owner := &SessionOwner{config: ServerConfig{Name: "required", Enabled: true, Required: true, ConnectAttempts: 1}, connect: func(context.Context, ServerConfig) (ClientSession, func() error, error) {
		return fake, func() error { closed = true; fake.closeN++; return nil }, nil
	}}
	if _, err := owner.Open(context.Background()); err == nil {
		t.Fatal("tools/list failure must fail session open")
	}
	if !closed || fake.closeN != 1 {
		t.Fatalf("failed session was not closed exactly once: closed=%v count=%d", closed, fake.closeN)
	}
}

func TestMCPOptionalOpenRetriesAfterFailure(t *testing.T) {
	attempts := 0
	fake := &p32FakeSession{}
	owner := &SessionOwner{config: ServerConfig{Name: "optional", Enabled: true, ConnectAttempts: 2, Backoff: 0}, connect: func(context.Context, ServerConfig) (ClientSession, func() error, error) {
		attempts++
		if attempts == 1 {
			return nil, nil, errors.New("temporary")
		}
		return fake, func() error { return nil }, nil
	}}
	if _, err := owner.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("connect attempts = %d, want 2", attempts)
	}
}

func TestMCPInitialOpenTimeoutReturnsContextError(t *testing.T) {
	owner := &SessionOwner{config: ServerConfig{Name: "optional", Enabled: true, ConnectAttempts: 2, Backoff: time.Second, Timeout: time.Millisecond}, connect: func(context.Context, ServerConfig) (ClientSession, func() error, error) {
		return nil, nil, errors.New("temporary")
	}}
	_, err := owner.Open(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v, want context deadline exceeded", err)
	}
}

func TestMCPBudgetToolReservesAndSettlesSharedHook(t *testing.T) {
	base := &p32Invokable{info: "search", result: "ok"}
	hook := &p32BudgetHook{}
	wrapped := &budgetTool{base: base, invokable: base, serverName: "inventory", toolName: "search", budget: hook, maxBytes: 10}
	if result, err := wrapped.InvokableRun(context.Background(), `{}`); err != nil || result != "ok" {
		t.Fatalf("wrapped tool result = %q, err=%v", result, err)
	}
	if hook.reserve != 1 || hook.settle != 1 || !hook.last.Succeeded {
		t.Fatalf("unexpected shared budget lifecycle: %#v", hook)
	}
}

type p32AddParams struct {
	X int `json:"x"`
	Y int `json:"y"`
}

func TestMCPOfficialSDKServerAndToolPolicy(t *testing.T) {
	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "p32-server", Version: "v1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "add", Description: "sum values"}, func(context.Context, *mcp.CallToolRequest, p32AddParams) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("%d", 3)}}}, nil, nil
	})
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "p32-client", Version: "v1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()
	tools, err := officialmcp.GetTools(ctx, &officialmcp.Config{
		Cli: clientSession, ServerName: "inventory", ToolNameList: []string{"add"},
		ToolNameMapper:    StableToolNameMapper("inventory"),
		DescriptionPolicy: &officialmcp.DescriptionPolicy{MaxChars: 5},
		ResultPolicy:      &officialmcp.ResultPolicy{MaxChars: 100},
	})
	if err != nil || len(tools) != 1 {
		t.Fatalf("official tool policy failed: len=%d err=%v", len(tools), err)
	}
	info, err := tools[0].Info(ctx)
	if err != nil || info.Name != "inventory__add" || len([]rune(info.Desc)) > 5 {
		t.Fatalf("official mapper/description policy failed: %#v err=%v", info, err)
	}
	result, err := tools[0].(tool.InvokableTool).InvokableRun(ctx, `{"x": 1, "y": 2}`)
	if err != nil || !strings.Contains(result, `"text":"3"`) {
		t.Fatalf("official tool invocation failed: %q err=%v", result, err)
	}
}

type p32Invokable struct{ info, result string }

func (t *p32Invokable) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.info}, nil
}
func (t *p32Invokable) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	return t.result, nil
}

type p32BudgetHook struct {
	reserve, settle int
	last            BudgetSettlement
}

func (h *p32BudgetHook) ReserveMCP(context.Context, BudgetRequest) error { h.reserve++; return nil }
func (h *p32BudgetHook) SettleMCP(_ context.Context, s BudgetSettlement) error {
	h.settle++
	h.last = s
	return nil
}

func TestMCPToolMapperAndSchemaHashAreStable(t *testing.T) {
	mapper := StableToolNameMapper("inventory")
	first, err := mapper(context.Background(), officialmcp.ToolNameMapperInput{ServerName: "inventory", Tool: mcp.Tool{Name: "search", Description: "find"}})
	if err != nil || first.ExposedName != "inventory__search" {
		t.Fatalf("unexpected stable mapping: %#v %v", first, err)
	}
	second, err := mapper(context.Background(), officialmcp.ToolNameMapperInput{ServerName: "inventory", Tool: mcp.Tool{Name: "search", Description: "find"}})
	if err != nil || first.ExposedName != second.ExposedName {
		t.Fatalf("mapping is not deterministic: %#v %#v", first, second)
	}
	hash1, err := MCPToolSchemaHash("inventory", "inventory__search", mcp.Tool{Name: "search", Description: "find", InputSchema: map[string]any{"type": "object"}})
	if err != nil {
		t.Fatal(err)
	}
	hash2, err := MCPToolSchemaHash("inventory", "inventory__search", mcp.Tool{Name: "search", Description: "changed", InputSchema: map[string]any{"type": "object"}})
	if err != nil || hash1 == hash2 || len(hash1) != 64 {
		t.Fatalf("schema hash does not cover server/tool description: %q %q", hash1, hash2)
	}
	if _, err := MCPToolCatalogHash("inventory", []mcp.Tool{{Name: "search"}, {Name: "search"}}, mapper); err == nil {
		t.Fatal("duplicate exposed tool names must fail catalog hashing")
	}
}

func TestMCPStdioCommandDoesNotInvokeShell(t *testing.T) {
	command := exec.Command("/bin/echo", "ok")
	if strings.Contains(strings.Join(command.Args, " "), "sh -c") {
		t.Fatal("test fixture unexpectedly invokes shell")
	}
}
