package runtime

// T4「webhook_out 真实公网 HTTPS」正式实验 harness。
//
// 调用链完全走生产代码：scripted 模型提出 webhook_out → Runtime/Approval →
// DurableExecutor resume → effects.Executor → ops/actions.WebhookOutAction →
// 真实公网 HTTPS endpoint。
//
// 证据分层：
//   * https://httpbin.org/anything      —— 规定用例：DNS + TLS + HTTPS + 2xx 真正发出；
//   * https://webhook.site/<token>      —— 服务端回显可检索：服务端真实收到的
//     Content-Type / Idempotency-Key / body 与 Effect 账本逐字对齐；
//   * https://httpbin.org/status/404|503 —— 4xx/5xx 分类（>=400 → definite failure）；
//   * https://httpbin.org/response-headers?X-Request-ID=... —— 响应头读取分支。
//
// 本文件不修改任何产品代码；所有 payload 均为测试专用假数据。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
	appconfig "SentinelOps/internal/config"
	"SentinelOps/internal/dao/mysql"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

const (
	t4EvidenceDirEnv  = "SENTINELOPS_T4_EVIDENCE_DIR"
	t4SuccessPayload  = `{"source":"sentinelops-interview-evidence","case":"webhook-success"}`
	t4HttpbinAnything = "https://httpbin.org/anything"
)

type t4EffectFact struct {
	EffectID          string `json:"effect_id"`
	IdempotencyKey    string `json:"idempotency_key"`
	Status            string `json:"status"`
	EffectType        string `json:"effect_type"`
	EffectStep        string `json:"effect_step"`
	ExternalReference string `json:"external_reference"`
	Attempt           uint   `json:"attempt"`
	LastError         string `json:"last_error,omitempty"`
	RequestRedacted   string `json:"request_redacted,omitempty"`
	ResponseRedacted  string `json:"response_redacted,omitempty"`
}

type t4CaseResult struct {
	Case           string         `json:"case"`
	TargetURL      string         `json:"target_url"`
	Payload        string         `json:"payload"`
	ApprovalIssued bool           `json:"approval_issued"`
	RunStatus      string         `json:"run_status"`
	RunAttempt     uint           `json:"run_attempt"`
	Effect         *t4EffectFact  `json:"effect,omitempty"`
	EventErrors    []string       `json:"runtime_event_errors,omitempty"`
	ServerEcho     map[string]any `json:"server_echo,omitempty"`
	Notes          string         `json:"notes,omitempty"`
}

// TestWebhookOutRealPublicHTTPS 是 T4 主实验。
func TestWebhookOutRealPublicHTTPS(t *testing.T) {
	evidenceRoot := strings.TrimSpace(os.Getenv(t4EvidenceDirEnv))
	if evidenceRoot == "" {
		t.Skip("T4 evidence harness: set " + t4EvidenceDirEnv + " to run the public HTTPS cases")
	}
	evidenceDir, err := filepath.Abs(evidenceRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(evidenceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// webhook_out 会读取 appconfig.Current()（判定是否注入 Authorization）。
	// 实验沿用现有确定性 harness 的做法注入空配置：secrets.effect 为空 →
	// 不发送 Authorization，与 preflight 记录的本地配置事实一致。
	oldConfig, _ := appconfig.Current()
	appconfig.SetCurrent(&appconfig.Config{})
	defer appconfig.SetCurrent(oldConfig)
	db := fixture12NewRuntimeDatabase(t, "t4_webhook_public")
	proxyFact := t4ProxyFact()

	// Case 1：httpbin.org/anything（用户指定 endpoint）——证明请求真正离开本机。
	httpbin := t4RunWebhookCase(t, db, "t4-httpbin-anything", t4HttpbinAnything, t4SuccessPayload)

	// Case 2：webhook.site 匿名 token ——服务端保存并可检索真实收到的 Header/Body。
	token, tokenErr := t4CreateWebhookSiteToken(t)
	webhookCase := t4CaseResult{Case: "t4-webhook-site", Notes: "token 创建失败:" + t4ErrText(tokenErr)}
	if tokenErr == nil {
		webhookCase = t4RunWebhookCase(t, db, "t4-webhook-site", token.Endpoint, t4SuccessPayload)
		if token.RetrievalErr == "" && webhookCase.Effect != nil {
			webhookCase.ServerEcho = t4FetchWebhookSiteRequests(t, token, webhookCase.Effect.IdempotencyKey)
		} else {
			webhookCase.Notes = token.RetrievalErr
		}
	}
	success := map[string]any{
		"proxy_env":    proxyFact,
		"httpbin":      httpbin,
		"webhook_site": webhookCase,
		"note": "httpbin 只回 200、不提供可检索回显；服务端真实收到值由 webhook.site 匿名 token 提供（headers/body 逐字对照）",
	}
	t4WriteJSON(t, filepath.Join(evidenceDir, "success.json"), success)

	// Case 3/4：真实公网 4xx / 5xx 分类。
	badRequest := t4RunWebhookCase(t, db, "t4-httpbin-404", "https://httpbin.org/status/404", t4SuccessPayload)
	serverError := t4RunWebhookCase(t, db, "t4-httpbin-503", "https://httpbin.org/status/503", t4SuccessPayload)
	// Case 5：响应 X-Request-ID 读取分支（由 httpbin 服务端设置响应头）。
	requestID := t4RunWebhookCase(t, db, "t4-httpbin-request-id",
		"https://httpbin.org/response-headers?X-Request-ID=t4-server-request-id", t4SuccessPayload)
	t4WriteJSON(t, filepath.Join(evidenceDir, "error-cases.json"), map[string]any{
		"http_404":          badRequest,
		"http_503":          serverError,
		"x_request_id_case": requestID,
		"note": "生产代码对 >=400 一律 classified 为 definite failure；X-Request-ID 分支验证响应头读取与 external_reference",
	})
	t.Logf("T4 evidence written to %s", evidenceDir)
}

func t4RunWebhookCase(t *testing.T, db *gorm.DB, suffix, targetURL, payload string) t4CaseResult {
	t.Helper()
	result := t4CaseResult{Case: suffix, TargetURL: targetURL, Payload: payload}
	store, ctx := fixture22RuntimeContext(t, db, suffix, "webhook_out", policy.RiskL2)
	fixture23InstallBudgetTruth(t, db, "run-phase22-"+suffix)
	modelDouble := &t4WebhookModel{url: targetURL, payload: payload}
	runner, attempt := fixture24NewExternalRunnerHarness(t, store, ctx, modelDouble)
	execution, err := InvokeRecoveryRunner(ctx, runner, attempt, RecoveryDecision{
		Mode: workflow.RecoveryModeReplay, ImmutableQuery: "send real public webhook",
	}, nil)
	if err != nil {
		result.EventErrors = append(result.EventErrors, "invoke: "+err.Error())
		return result
	}
	var interrupts []*adk.InterruptCtx
	for {
		event, ok := execution.Events.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			result.EventErrors = append(result.EventErrors, event.Err.Error())
		}
		if event.Action != nil && event.Action.Interrupted != nil {
			interrupts = event.Action.Interrupted.InterruptContexts
		}
	}
	if len(interrupts) == 0 {
		result.EventErrors = append(result.EventErrors, "external Tool call emitted no Approval interrupt")
		return result
	}
	durable := &DurableExecutor{store: store}
	if err := durable.publishApprovalInterrupt(ctx, attempt, interrupts); err != nil {
		result.EventErrors = append(result.EventErrors, "publish approval: "+err.Error())
		return result
	}
	var approval mysql.AgentApproval
	if err := db.First(&approval, "run_id = ?", attempt.Run.ID).Error; err != nil {
		result.EventErrors = append(result.EventErrors, "read approval: "+err.Error())
		return result
	}
	approverID := "approver-" + suffix
	approverCtx := policy.WithIdentity(context.Background(), policy.Identity{
		UserID: approverID, Role: policy.RoleApprover, Scope: policy.Scope{UserID: approverID},
	})
	if _, err := store.DecideApprovalAndWakeRun(approverCtx, workflow.DecideApprovalInput{
		ApprovalID: approval.ID, ProposalHash: approval.ProposalHash,
		ExpectedVersion: approval.Version, Decision: workflow.ApprovalStatusApproved,
	}); err != nil {
		result.EventErrors = append(result.EventErrors, "approve: "+err.Error())
		return result
	}
	result.ApprovalIssued = true
	claimed, ok, err := store.ClaimNextRun(context.Background(), workflow.ClaimInput{
		Owner: "worker-" + suffix, LeaseDuration: time.Hour,
	})
	if err != nil || !ok || claimed.Run.ID != attempt.Run.ID {
		result.EventErrors = append(result.EventErrors,
			fmt.Sprintf("claim approved external run: ok=%v err=%v", ok, t4ErrText(err)))
		return t4FinalizeCase(t, db, result, attempt.Run.ID)
	}
	budgets, err := NewDurableBudget(store)
	if err != nil {
		t.Fatal(err)
	}
	resumeCtx, resumeAttempt, err := BuildAttemptContext(context.Background(), *claimed, budgets)
	if err != nil {
		result.EventErrors = append(result.EventErrors, "build resume attempt: "+err.Error())
		return t4FinalizeCase(t, db, result, attempt.Run.ID)
	}
	modelSnapshot := resumeAttempt.Snapshot.Models()[0]
	resumeCtx, err = WithModelInvocation(resumeCtx, ModelInvocation{
		ReservationIdentity: "resume-model-" + suffix, CatalogRef: modelSnapshot.CatalogRef,
		Provider: modelSnapshot.Provider, Driver: modelSnapshot.Driver, ModelID: modelSnapshot.ModelID,
		Profile: modelSnapshot.Profile, SnapshotIdentity: modelSnapshot.Identity(),
	})
	if err != nil {
		result.EventErrors = append(result.EventErrors, "model invocation: "+err.Error())
		return t4FinalizeCase(t, db, result, attempt.Run.ID)
	}
	params, err := durable.approvalResumeParams(resumeCtx, resumeAttempt)
	if err != nil {
		result.EventErrors = append(result.EventErrors, "resume params: "+err.Error())
		return t4FinalizeCase(t, db, result, attempt.Run.ID)
	}
	checkpointID, err := workflow.EinoCheckpointID(attempt.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := InvokeRecoveryRunner(resumeCtx, runner, resumeAttempt, RecoveryDecision{
		Mode: workflow.RecoveryModeResume, CheckpointID: checkpointID,
	}, params)
	if err != nil {
		result.EventErrors = append(result.EventErrors, "resume invoke: "+err.Error())
		return t4FinalizeCase(t, db, result, attempt.Run.ID)
	}
	for {
		event, ok := resumed.Events.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			result.EventErrors = append(result.EventErrors, "resume event: "+event.Err.Error())
		}
	}
	return t4FinalizeCase(t, db, result, attempt.Run.ID)
}

func t4FinalizeCase(t *testing.T, db *gorm.DB, result t4CaseResult, runID string) t4CaseResult {
	t.Helper()
	var run mysql.WorkflowRun
	if err := db.First(&run, "id = ?", runID).Error; err == nil {
		result.RunStatus = run.Status
		result.RunAttempt = run.Attempt
	}
	var effect mysql.AgentEffect
	if err := db.First(&effect, "run_id = ? AND effect_step = ?", runID, workflow.EffectStepPrimary).Error; err == nil {
		fact := &t4EffectFact{
			EffectID: effect.ID, IdempotencyKey: effect.IdempotencyKey, Status: effect.Status,
			EffectType: effect.EffectType, EffectStep: effect.EffectStep, Attempt: effect.Attempt,
			ExternalReference: t4Deref(effect.ExternalReference), LastError: t4Deref(effect.LastError),
			RequestRedacted: t4Deref(effect.RequestRedacted), ResponseRedacted: t4Deref(effect.ResponseRedacted),
		}
		result.Effect = fact
	} else {
		result.EventErrors = append(result.EventErrors, "read effect: "+t4ErrText(err))
	}
	return result
}

type t4WebhookModel struct {
	url     string
	payload string
	calls   int
}

func (m *t4WebhookModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	m.calls++
	if m.calls == 1 {
		arguments, err := json.Marshal(map[string]string{"url": m.url, "payload": m.payload, "method": "POST"})
		if err != nil {
			return nil, err
		}
		return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{
			ID: "t4-webhook-call", Type: "function",
			Function: schema.FunctionCall{Name: "webhook_out", Arguments: string(arguments)},
		}}}, nil
	}
	return schema.AssistantMessage("webhook persisted", nil), nil
}

func (m *t4WebhookModel) Stream(ctx context.Context, input []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, input, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func (*t4WebhookModel) BindTools([]*schema.ToolInfo) error { return nil }

type t4WebhookSiteToken struct {
	UUID         string
	Endpoint     string
	RetrievalErr string
}

func t4CreateWebhookSiteToken(t *testing.T) (t4WebhookSiteToken, error) {
	t.Helper()
	token := t4WebhookSiteToken{}
	client := &http.Client{Timeout: 20 * time.Second}
	request, err := http.NewRequest(http.MethodPost, "https://webhook.site/token", strings.NewReader("{}"))
	if err != nil {
		return token, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return token, fmt.Errorf("create webhook.site token: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return token, err
	}
	if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusOK {
		return token, fmt.Errorf("webhook.site token status=%d", response.StatusCode)
	}
	var payload struct {
		UUID string `json:"uuid"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return token, fmt.Errorf("decode webhook.site token: %w", err)
	}
	if payload.UUID == "" {
		return token, fmt.Errorf("webhook.site token response has no uuid")
	}
	token.UUID = payload.UUID
	token.Endpoint = "https://webhook.site/" + payload.UUID
	return token, nil
}

func t4FetchWebhookSiteRequests(t *testing.T, token t4WebhookSiteToken, expectIdempotencyKey string) map[string]any {
	t.Helper()
	client := &http.Client{Timeout: 20 * time.Second}
	deadline := time.Now().Add(30 * time.Second)
	var lastErr string
	for time.Now().Before(deadline) {
		url := fmt.Sprintf("https://webhook.site/token/%s/requests?sorting=newest", token.UUID)
		response, err := client.Get(url)
		if err != nil {
			lastErr = err.Error()
			time.Sleep(time.Second)
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		_ = response.Body.Close()
		if readErr != nil {
			lastErr = readErr.Error()
			time.Sleep(time.Second)
			continue
		}
		var payload struct {
			Total int `json:"total"`
			Data  []struct {
				Method  string              `json:"method"`
				URL     string              `json:"url"`
				Content string              `json:"content"`
				Headers map[string][]string `json:"headers"`
				Size    int                 `json:"size"`
				Time    float64             `json:"time"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			lastErr = err.Error()
			time.Sleep(time.Second)
			continue
		}
		for _, item := range payload.Data {
			keys := item.Headers["idempotency-key"]
			if len(keys) == 0 {
				continue
			}
			contentType := firstOrEmpty(item.Headers["content-type"])
			return map[string]any{
				"retrieved_requests": payload.Total,
				"method":             item.Method,
				"url":                item.URL,
				"content_type":       contentType,
				"idempotency_key":    keys[0],
				"idempotency_key_matches_effect_key": keys[0] == expectIdempotencyKey,
				"body":               item.Content,
				"body_matches_payload": item.Content == t4SuccessPayload,
				"user_agent":         firstOrEmpty(item.Headers["user-agent"]),
				"x_request_id_sent":  len(item.Headers["x-request-id"]) > 0,
				"raw_header_names":   t4HeaderNames(item.Headers),
			}
		}
		lastErr = "token has no request with Idempotency-Key yet"
		time.Sleep(time.Second)
	}
	return map[string]any{"error": lastErr}
}

func t4HeaderNames(headers map[string][]string) []string {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	return names
}

func firstOrEmpty(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func t4ProxyFact() map[string]any {
	fact := map[string]any{}
	for _, name := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "NO_PROXY"} {
		if value, ok := os.LookupEnv(name); ok && value != "" {
			fact[name] = value
		}
	}
	return fact
}

func t4Deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func t4ErrText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func t4WriteJSON(t *testing.T, path string, payload any) {
	t.Helper()
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("encode %s: %v", filepath.Base(path), err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
