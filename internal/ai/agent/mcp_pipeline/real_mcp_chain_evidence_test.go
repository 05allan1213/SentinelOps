package mcp_pipeline

// T3「真实 MCP 链路」正式实验 harness（第一部分：真实 Context7 Server）。
//
// 真实部分：production SessionOwner（officialmcp/streamable-http transport、真实
// initialize、真实 tools/list）、production Tool Schema、production 只读门禁
// （resolveDynamicTools / isReadOnlyMCPTool）、production ResultObserver、真实
// tools/call 打到 127.0.0.1:3333 的 Context7 Server。
//
// 拒绝 Case：
//   A. allowed_tools 白名单外 Tool：服务端 tools/list 返回 2 个工具，Runtime 只暴露
//      1 个；对白名单外工具名的请求由 production UnknownToolHandler 拒绝，计数代理
//      证明期间 0 个 tools/call 到达服务端。
//   B. 非只读 Tool：in-process MCP Server 暴露一个远端自称"安全但可写"的工具，
//      production resolveDynamicTools 在真正发送 tools/call 前拒绝（trap 计数为 0）。
//
// 本文件不修改任何产品代码。

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"SentinelOps/internal/ai/policy"
	aitools "SentinelOps/internal/ai/tools"
	appconfig "SentinelOps/internal/config"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	mcptools "SentinelOps/internal/ai/tools/mcp"
)

const (
	t3EvidenceDirEnv = "SENTINELOPS_T3_EVIDENCE_DIR"
	t3ServerName     = "context7"
	t3ProbeArgs      = `{"libraryName":"Go","query":"standard library http server"}`
)

type t3ToolFact struct {
	RawName       string          `json:"raw_name"`
	Description   string          `json:"description,omitempty"`
	InputSchema   json.RawMessage `json:"input_schema,omitempty"`
	ReadOnlyHint  bool            `json:"read_only_hint"`
	Destructive   bool            `json:"destructive_hint"`
	ExposedName   string          `json:"exposed_name,omitempty"`
	SchemaHash    string          `json:"schema_hash,omitempty"` // policy.ToolSchemaHash（Runtime 侧目录哈希输入）
	AnnotationsIn string         `json:"annotations_source,omitempty"`
}

type t3CallResult struct {
	Tool          string `json:"tool"`
	ExposedName   string `json:"exposed_name"`
	Input         string `json:"input_json"`
	DurationMS    int64  `json:"duration_ms"`
	Success       bool   `json:"success"`
	Error         string `json:"error,omitempty"`
	ResultChars   int64  `json:"result_chars"`
	ResultBytes   int64  `json:"result_bytes"`
	ResultSHA256  string `json:"result_sha256"`
	ResultExcerpt string `json:"result_excerpt"`
	LibraryIDs    []string `json:"observed_library_ids,omitempty"`
}

type t3Observation struct {
	Phase     string `json:"phase"`
	StartedAt string `json:"started_at"`
	EndedAt   string `json:"ended_at"`
	DurationMS int64 `json:"duration_ms"`
	Detail    string `json:"detail,omitempty"`
	Success   bool   `json:"success"`
}

// TestMCPRealContext7Chain 是 T3 主实验：真实 Context7 MCP 链路。
func TestMCPRealContext7Chain(t *testing.T) {
	evidenceDir := t3EvidenceDir(t)
	server := t3Context7ServerConfig(t)

	// ---------- Happy path：真实 connect → initialize → tools/list → tools/call ----------
	observations := make([]t3Observation, 0, 8)
	observerUsage := make([]mcptools.ResultUsage, 0, 4)
	server.ObserveResult = func(_ context.Context, usage mcptools.ResultUsage) {
		observerUsage = append(observerUsage, usage)
	}
	owner, err := mcptools.NewSessionOwner(server)
	if err != nil {
		t.Fatalf("build production SessionOwner: %v", err)
	}
	defer func() { _ = owner.Close() }()

	connectStart := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	session, err := owner.Open(ctx)
	connectEnd := time.Now()
	observations = append(observations, t3Observation{
		Phase: "connect+initialize+tools/list(probe)", StartedAt: connectStart.UTC().Format(time.RFC3339Nano),
		EndedAt: connectEnd.UTC().Format(time.RFC3339Nano), DurationMS: connectEnd.Sub(connectStart).Milliseconds(), Success: err == nil,
		Detail: fmt.Sprintf("endpoint=%s transport=%s", server.Transport.URL, server.Transport.Type),
	})
	if err != nil {
		t.Fatalf("production MCP session connect failed: %v", err)
	}
	if session == nil {
		t.Fatal("production MCP session is nil")
	}

	listStart := time.Now()
	rawList, err := session.ListTools(ctx, nil)
	listEnd := time.Now()
	observations = append(observations, t3Observation{
		Phase: "tools/list(server raw)", StartedAt: listStart.UTC().Format(time.RFC3339Nano),
		EndedAt: listEnd.UTC().Format(time.RFC3339Nano), DurationMS: listEnd.Sub(listStart).Milliseconds(), Success: err == nil,
	})
	if err != nil {
		t.Fatalf("raw tools/list failed: %v", err)
	}
	rawTools := make([]t3ToolFact, 0, len(rawList.Tools))
	for _, raw := range rawList.Tools {
		destructive := false
		if raw.Annotations != nil && raw.Annotations.DestructiveHint != nil {
			destructive = *raw.Annotations.DestructiveHint
		}
		rawTools = append(rawTools, t3ToolFact{
			RawName: raw.Name, Description: raw.Description, InputSchema: t3MarshalJSON(t, raw.InputSchema),
			ReadOnlyHint: raw.Annotations != nil && raw.Annotations.ReadOnlyHint,
			Destructive:  destructive,
		})
	}

	exposeStart := time.Now()
	exposedTools, err := owner.Tools(ctx)
	exposeEnd := time.Now()
	observations = append(observations, t3Observation{
		Phase: "officialmcp.GetTools(allowlist+policy)", StartedAt: exposeStart.UTC().Format(time.RFC3339Nano),
		EndedAt: exposeEnd.UTC().Format(time.RFC3339Nano), DurationMS: exposeEnd.Sub(exposeStart).Milliseconds(), Success: err == nil,
	})
	if err != nil {
		t.Fatalf("production tools() failed: %v", err)
	}

	// production 只读门禁 + 目录哈希：必须返回已批准只读目录。
	catalogStart := time.Now()
	dynamicTools, catalog, catalogErr := resolveDynamicTools(ctx, Config{Source: StaticToolSource(exposedTools)})
	catalogEnd := time.Now()
	observations = append(observations, t3Observation{
		Phase: "resolveDynamicTools(read-only gate + catalog hash)", StartedAt: catalogStart.UTC().Format(time.RFC3339Nano),
		EndedAt: catalogEnd.UTC().Format(time.RFC3339Nano), DurationMS: catalogEnd.Sub(catalogStart).Milliseconds(),
		Success: catalogErr == nil, Detail: fmt.Sprintf("tools=%d catalog_hash=%s", len(dynamicTools), catalog.Hash),
	})
	if catalogErr != nil {
		t.Fatalf("production read-only gate rejected the real context7 catalog: %v", catalogErr)
	}

	exposedFacts := make([]t3ToolFact, 0, len(exposedTools))
	for _, base := range exposedTools {
		info, infoErr := base.Info(ctx)
		if infoErr != nil {
			t.Fatalf("read exposed tool info: %v", infoErr)
		}
		exposedFacts = append(exposedFacts, t3ToolFact{
			RawName: info.Name, Description: info.Desc, ExposedName: info.Name,
			SchemaHash: t3SchemaHash(t, info),
		})
	}
	t3WriteJSON(t, filepath.Join(evidenceDir, "tools-list.json"), map[string]any{
		"server": map[string]any{
			"name": t3ServerName, "transport": server.Transport.Type, "endpoint": server.Transport.URL,
			"allowed_tools": server.AllowedTools, "allowed_hosts": server.AllowedHosts, "allowed_ports": server.AllowedPorts,
		},
		"server_raw_tools":           rawTools,
		"runtime_exposed_tools":      exposedFacts,
		"runtime_catalog_hash":       catalog.Hash,
		"runtime_catalog_entries":    catalog.Entries,
		"observed_at":                time.Now().UTC().Format(time.RFC3339Nano),
	})

	target := t3SelectTool(t, exposedTools, "resolve-library-id")
	targetInfo, err := target.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	invokable, ok := target.(tool.InvokableTool)
	if !ok {
		t.Fatalf("exposed tool %q is not invokable", targetInfo.Name)
	}
	callStart := time.Now()
	result, callErr := invokable.InvokableRun(ctx, t3ProbeArgs)
	callEnd := time.Now()
	call := t3CallResult{
		Tool: "resolve-library-id", ExposedName: targetInfo.Name, Input: t3ProbeArgs,
		DurationMS: callEnd.Sub(callStart).Milliseconds(), Success: callErr == nil,
		ResultChars: int64(len([]rune(result))), ResultBytes: int64(len(result)),
	}
	if callErr != nil {
		call.Error = callErr.Error()
	} else {
		digest := sha256.Sum256([]byte(result))
		call.ResultSHA256 = hex.EncodeToString(digest[:])
		call.ResultExcerpt = t3Truncate(result, 600)
		call.LibraryIDs = t3LibraryIDs(result)
	}
	observations = append(observations, t3Observation{
		Phase: "tools/call", StartedAt: callStart.UTC().Format(time.RFC3339Nano),
		EndedAt: callEnd.UTC().Format(time.RFC3339Nano), DurationMS: call.DurationMS, Success: call.Success,
		Detail: fmt.Sprintf("%s %s", targetInfo.Name, t3ProbeArgs),
	})
	t3WriteJSON(t, filepath.Join(evidenceDir, "tool-call.json"), map[string]any{
		"call": call, "observer_usage": observerUsage,
		"note": "调用经 production SessionOwner/officialmcp InvokableTool；结果只保留摘要与哈希，不落第三方原文",
	})

	// ---------- 拒绝 Case A：白名单外 Tool（真实 Server + 计数代理） ----------
	rejectionAllowlist := t3AllowlistRejectionCase(t, ctx, &observations)
	// ---------- 拒绝 Case B：非只读 Tool（in-process trap server） ----------
	rejectionWrite := t3WriteToolRejectionCase(t, ctx)
	t3WriteJSON(t, filepath.Join(evidenceDir, "policy-rejection.json"), map[string]any{
		"allowlist_case": rejectionAllowlist,
		"write_tool_case": rejectionWrite,
		"note": "两个 Case 都在 Runtime 策略层拒绝；trap/代理计数证明拒绝发生在 tools/call 之前",
	})

	t3WriteJSON(t, filepath.Join(evidenceDir, "trace.json"), map[string]any{
		"endpoint":      server.Transport.URL,
		"server":        t3ServerName,
		"observations":  observations,
		"observer_usage": observerUsage,
		"note": "该 trace 是 harness 在 production 观测点（Session/ResultObserver）采集的调用时间线；" +
			"不是 agent_trace_nodes 持久化行（无 durable Attempt/GoFrame trace store 上下文，本轮不伪造 Trace 落库）",
	})
	t.Logf("T3 context7 chain evidence written to %s", evidenceDir)
}

func t3Context7ServerConfig(t *testing.T) mcptools.ServerConfig {
	t.Helper()
	repoRoot := t3RepoRoot(t)
	cfg, path, err := appconfig.LoadDirectory(filepath.Join(repoRoot, "manifest", "config"))
	if err != nil {
		t.Fatalf("load app config: %v", err)
	}
	runtimeCfg, err := mcptools.FromAppConfig(cfg)
	if err != nil {
		t.Fatalf("mcp FromAppConfig: %v", err)
	}
	for _, server := range runtimeCfg.Servers {
		if server.Name == t3ServerName {
			t.Logf("T3 using production MCP config %s: endpoint=%s tools=%v", path, server.Transport.URL, server.AllowedTools)
			return server
		}
	}
	t.Fatalf("mcp server %q is not configured", t3ServerName)
	return mcptools.ServerConfig{}
}

type t3CountingProxy struct {
	server   *httptest.Server
	mu       sync.Mutex
	methods  map[string]int
	requests []string
}

func (p *t3CountingProxy) URL() string { return p.server.URL }

func (p *t3CountingProxy) count(method string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.methods == nil {
		p.methods = map[string]int{}
	}
	if method == "" {
		method = "<none>"
	}
	p.methods[method]++
	p.requests = append(p.requests, method)
}

func (p *t3CountingProxy) snapshot() (map[string]int, []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	methods := make(map[string]int, len(p.methods))
	for key, value := range p.methods {
		methods[key] = value
	}
	return methods, append([]string(nil), p.requests...)
}

func t3NewCountingProxy(t *testing.T, targetRaw string) *t3CountingProxy {
	t.Helper()
	target, err := url.Parse(targetRaw)
	if err != nil {
		t.Fatalf("parse proxy target: %v", err)
	}
	// 只保留 scheme://host，路径由请求侧决定，避免 ReverseProxy 拼出 /mcp/mcp。
	target.Path = ""
	reverse := httputil.NewSingleHostReverseProxy(target)
	proxy := &t3CountingProxy{methods: map[string]int{}}
	proxy.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(body))
		var envelope struct {
			Method string `json:"method"`
		}
		if len(body) > 0 {
			_ = json.Unmarshal(body, &envelope)
		}
		proxy.count(envelope.Method)
		reverse.ServeHTTP(w, r)
	}))
	t.Cleanup(proxy.server.Close)
	return proxy
}

func t3AllowlistRejectionCase(t *testing.T, ctx context.Context, observations *[]t3Observation) map[string]any {
	t.Helper()
	base := t3Context7ServerConfig(t)
	proxy := t3NewCountingProxy(t, base.Transport.URL)
	proxyURL, err := url.Parse(proxy.URL())
	if err != nil {
		t.Fatal(err)
	}
	port := 0
	if _, portText, splitErr := net.SplitHostPort(proxyURL.Host); splitErr == nil {
		_, _ = fmt.Sscanf(portText, "%d", &port)
	}
	server := base
	server.AllowedTools = []string{"resolve-library-id"}
	server.Transport.URL = proxy.URL() + "/mcp"
	server.AllowedHosts = []string{"127.0.0.1"}
	server.AllowedPorts = []int{port}
	owner, err := mcptools.NewSessionOwner(server)
	if err != nil {
		t.Fatalf("build allowlist SessionOwner: %v", err)
	}
	defer func() { _ = owner.Close() }()
	start := time.Now()
	session, err := owner.Open(ctx)
	if err != nil {
		t.Fatalf("allowlist session open: %v", err)
	}
	rawList, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("allowlist raw tools/list: %v", err)
	}
	exposed, err := owner.Tools(ctx)
	if err != nil {
		t.Fatalf("allowlist exposed tools: %v", err)
	}
	names, err := aitools.ToolNames(ctx, exposed)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	serverRawNames := make([]string, 0, len(rawList.Tools))
	for _, raw := range rawList.Tools {
		serverRawNames = append(serverRawNames, raw.Name)
	}
	sort.Strings(serverRawNames)
	excluded := ""
	for _, rawName := range serverRawNames {
		if rawName != "resolve-library-id" {
			excluded = rawName
			break
		}
	}
	// production Tool Node 的未知工具门禁：模型请求白名单外工具名。
	gate := aitools.UnknownToolHandler(names)
	gateMessage, gateErr := gate(ctx, "context7__"+excluded, "{}")
	duration := time.Since(start).Milliseconds()
	methods, requests := proxy.snapshot()
	*observations = append(*observations, t3Observation{
		Phase: "policy rejection: allowlist", StartedAt: start.UTC().Format(time.RFC3339Nano),
		EndedAt: time.Now().UTC().Format(time.RFC3339Nano), DurationMS: duration, Success: gateErr == nil && methods["tools/call"] == 0,
		Detail: fmt.Sprintf("excluded=%s tool_call_requests=%d", excluded, methods["tools/call"]),
	})
	return map[string]any{
		"server_raw_tool_names":     serverRawNames,
		"runtime_exposed_tool_names": names,
		"excluded_tool":              "context7__" + excluded,
		"unknown_tool_gate_result":   gateMessage,
		"unknown_tool_gate_error":    t3ErrorText(gateErr),
		"proxy_method_counts":        methods,
		"proxy_request_sequence":     requests,
		"tools_call_requests":        methods["tools/call"],
		"note": "tool 由 production allowed_tools 白名单决定是否暴露；未知工具请求由 production UnknownToolHandler 拒绝，" +
			"代理观测期间 tools/call = 0",
	}
}

func t3WriteToolRejectionCase(t *testing.T, ctx context.Context) map[string]any {
	t.Helper()
	counters := &t3TrapCounters{methods: map[string]int{}}
	server := mcp.NewServer(&mcp.Implementation{Name: "t3-trap-server", Version: "v1"}, nil)
	destructive := true
	mcp.AddTool(server, &mcp.Tool{
		Name: "inventory_search", Description: "looks safe by name but is not read-only",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &destructive},
	}, func(context.Context, *mcp.CallToolRequest, map[string]any) (*mcp.CallToolResult, any, error) {
		counters.toolCallExecuted++
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "write happened"}}}, nil, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(body))
		var envelope struct {
			Method string `json:"method"`
		}
		if len(body) > 0 {
			_ = json.Unmarshal(body, &envelope)
		}
		counters.record(envelope.Method)
		handler.ServeHTTP(w, r)
	}))
	defer ts.Close()
	parsed, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	port := 0
	if _, portText, splitErr := net.SplitHostPort(parsed.Host); splitErr == nil {
		_, _ = fmt.Sscanf(portText, "%d", &port)
	}
	serverConfig := mcptools.ServerConfig{
		Name: "trap", Enabled: true, AllowedTools: []string{"inventory_search"}, ToolNamespace: "trap",
		Transport: mcptools.TransportConfig{Type: mcptools.TransportStreamableHTTP, URL: ts.URL},
		AllowedHosts: []string{"127.0.0.1"}, AllowedPorts: []int{port}, AllowInsecureTLSForTest: true,
		MaxToolPages: 1,
	}
	owner, err := mcptools.NewSessionOwner(serverConfig)
	if err != nil {
		t.Fatalf("build trap SessionOwner: %v", err)
	}
	defer func() { _ = owner.Close() }()
	start := time.Now()
	exposed, err := owner.Tools(ctx)
	if err != nil {
		t.Fatalf("trap tools(): %v", err)
	}
	_, _, gateErr := resolveDynamicTools(ctx, Config{Source: StaticToolSource(exposed)})
	duration := time.Since(start).Milliseconds()
	methods, _ := counters.snapshot()
	return map[string]any{
		"trap_tool":            "inventory_search",
		"remote_annotation":    map[string]any{"readOnlyHint": false, "destructiveHint": true},
		"runtime_rejection":    t3ErrorText(gateErr),
		"rejection_happened":   gateErr != nil,
		"transport_methods":    methods,
		"tools_call_requests":  methods["tools/call"],
		"tool_body_executions": counters.toolCallExecuted,
		"duration_ms":          duration,
		"note": "远端工具通过 tools/list 可见且在白名单内，但 readOnlyHint=false，" +
			"production resolveDynamicTools 在构建目录阶段拒绝；trap 观测 tools/call = 0",
	}
}

type t3TrapCounters struct {
	mu               sync.Mutex
	methods          map[string]int
	toolCallExecuted int
}

func (c *t3TrapCounters) record(method string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.methods[method]++
}

func (c *t3TrapCounters) snapshot() (map[string]int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	methods := make(map[string]int, len(c.methods))
	for key, value := range c.methods {
		methods[key] = value
	}
	return methods, c.toolCallExecuted
}

func t3SelectTool(t *testing.T, tools []tool.BaseTool, contains string) tool.BaseTool {
	t.Helper()
	ctx := context.Background()
	for _, base := range tools {
		info, err := base.Info(ctx)
		if err != nil {
			t.Fatalf("tool info: %v", err)
		}
		if strings.Contains(info.Name, contains) {
			return base
		}
	}
	t.Fatalf("no exposed tool contains %q", contains)
	return nil
}

func t3SchemaHash(t *testing.T, info *schema.ToolInfo) string {
	t.Helper()
	hash, err := policy.ToolSchemaHash(info)
	if err != nil {
		t.Fatalf("tool schema hash: %v", err)
	}
	return hash
}

func t3LibraryIDs(result string) []string {
	pattern := regexp.MustCompile(`/[A-Za-z0-9_.\-]+/[A-Za-z0-9_.\-]+`)
	seen := map[string]bool{}
	ids := make([]string, 0, 8)
	for _, match := range pattern.FindAllString(result, -1) {
		if seen[match] {
			continue
		}
		seen[match] = true
		ids = append(ids, match)
		if len(ids) == 8 {
			break
		}
	}
	return ids
}

func t3Truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}

func t3MarshalJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	if value == nil {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return encoded
}

func t3ErrorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func t3RepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "manifest", "config", "config.local.yaml")); err != nil {
		t.Fatalf("config.local.yaml not found from %s: %v", root, err)
	}
	return root
}

func t3EvidenceDir(t *testing.T) string {
	t.Helper()
	root := strings.TrimSpace(os.Getenv(t3EvidenceDirEnv))
	if root == "" {
		t.Skip("T3 evidence harness: set " + t3EvidenceDirEnv + " to run the real MCP chain")
	}
	dir, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func t3WriteJSON(t *testing.T, path string, payload any) {
	t.Helper()
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("encode %s: %v", filepath.Base(path), err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
