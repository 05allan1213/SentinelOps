package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
)

// HTTPRuntimeAdapter 只调用生产 durable Run API，不直接接触 Agent 或 Worker。
type HTTPRuntimeAdapter struct {
	BaseURL         string
	Client          *http.Client
	Header          http.Header
	IdentityHeaders map[ExecutionIdentity]http.Header
	Faults          ScenarioFaultController
}

type createRunRequest struct {
	SessionID string `json:"session_id"`
	Query     string `json:"query"`
	Agent     string `json:"agent,omitempty"`
}

type createRunResponse struct {
	Data      *RunHandle `json:"data"`
	RunID     string     `json:"run_id"`
	SessionID string     `json:"session_id"`
	Status    string     `json:"status"`
}

// NewHTTPRuntimeAdapter 创建生产 API adapter。
func NewHTTPRuntimeAdapter(baseURL string, client *http.Client, header http.Header) (*HTTPRuntimeAdapter, error) {
	return newHTTPRuntimeAdapter(baseURL, client, header, nil)
}

// NewHTTPRuntimeAdapterWithIdentities 为固定 Eval 身份分别绑定请求 Header。
func NewHTTPRuntimeAdapterWithIdentities(baseURL string, client *http.Client, headers map[ExecutionIdentity]http.Header) (*HTTPRuntimeAdapter, error) {
	return newHTTPRuntimeAdapter(baseURL, client, nil, headers)
}

func newHTTPRuntimeAdapter(baseURL string, client *http.Client, header http.Header, headers map[ExecutionIdentity]http.Header) (*HTTPRuntimeAdapter, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("invalid production API base URL")
	}
	if client == nil {
		client = http.DefaultClient
	}
	identityHeaders := make(map[ExecutionIdentity]http.Header, len(headers))
	for identity, identityHeader := range headers {
		if !identity.valid() {
			return nil, fmt.Errorf("unsupported eval execution identity %q", identity)
		}
		identityHeaders[identity] = cloneHeader(identityHeader)
	}
	return &HTTPRuntimeAdapter{
		BaseURL: strings.TrimRight(parsed.String(), "/"), Client: client, Header: cloneHeader(header), IdentityHeaders: identityHeaders,
	}, nil
}

// Submit 创建一个 durable Run；SessionID 缺省时生成长度受限的隔离 Session。
func (a *HTTPRuntimeAdapter) Submit(ctx context.Context, item EvalCase) (RunHandle, error) {
	if a == nil || a.Client == nil {
		return RunHandle{}, fmt.Errorf("production API adapter is not initialized")
	}
	sessionID := strings.TrimSpace(item.SessionID)
	if sessionID == "" {
		sessionID = "eval-" + uuid.NewString()
	}
	payload, err := json.Marshal(createRunRequest{SessionID: sessionID, Query: item.Query, Agent: item.Agent})
	if err != nil {
		return RunHandle{}, fmt.Errorf("marshal production Run request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, a.BaseURL+"/api/chat/v2/runs", bytes.NewReader(payload))
	if err != nil {
		return RunHandle{}, fmt.Errorf("build production Run request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	header, err := a.headerFor(item.ExecutionIdentity)
	if err != nil {
		return RunHandle{}, err
	}
	for key, values := range header {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	response, err := a.Client.Do(request)
	if err != nil {
		return RunHandle{}, fmt.Errorf("submit production Run: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return RunHandle{}, fmt.Errorf("production Run API returned HTTP %d", response.StatusCode)
	}
	var payloadResponse createRunResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&payloadResponse); err != nil {
		return RunHandle{}, fmt.Errorf("decode production Run response: %w", err)
	}
	handle := RunHandle{RunID: payloadResponse.RunID, SessionID: payloadResponse.SessionID, Status: payloadResponse.Status}
	if payloadResponse.Data != nil {
		handle = *payloadResponse.Data
	}
	if handle.RunID == "" {
		return RunHandle{}, fmt.Errorf("production Run response has no run_id")
	}
	if handle.SessionID == "" {
		handle.SessionID = sessionID
	}
	return handle, nil
}

// Close 清除 adapter 生命周期内暂存的请求 Header，尤其是 Authorization。
func (a *HTTPRuntimeAdapter) Close() {
	if a == nil {
		return
	}
	for key := range a.Header {
		a.Header.Del(key)
	}
	a.Header = nil
	for identity, header := range a.IdentityHeaders {
		for key := range header {
			header.Del(key)
		}
		delete(a.IdentityHeaders, identity)
	}
	a.IdentityHeaders = nil
}

func (a *HTTPRuntimeAdapter) headerFor(identity ExecutionIdentity) (http.Header, error) {
	if identity == "" || len(a.IdentityHeaders) == 0 {
		return a.Header, nil
	}
	header, ok := a.IdentityHeaders[identity]
	if !ok {
		return nil, fmt.Errorf("authorization header is not configured for eval identity %q", identity)
	}
	return header, nil
}

func cloneHeader(header http.Header) http.Header {
	if header == nil {
		return make(http.Header)
	}
	return header.Clone()
}
