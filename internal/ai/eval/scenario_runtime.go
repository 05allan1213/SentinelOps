package eval

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type approvalAPIItem struct {
	ID           string `json:"id"`
	RunID        string `json:"run_id"`
	ProposalHash string `json:"proposal_hash"`
	RequestedBy  string `json:"requested_by"`
	Status       string `json:"status"`
	Version      uint64 `json:"version"`
}

type approvalAPIEnvelope struct {
	Data *struct {
		Item approvalAPIItem `json:"item"`
	} `json:"data"`
}

type approvalDecisionRequest struct {
	ProposalHash string `json:"proposal_hash"`
	Version      uint64 `json:"version"`
	Reason       string `json:"reason"`
}

// DriveScenario 使用公开 Approval API 和显式本地故障控制器驱动非普通 Case。
func (a *HTTPRuntimeAdapter) DriveScenario(
	ctx context.Context,
	item EvalCase,
	handle RunHandle,
	probe ScenarioProbe,
) (func() error, error) {
	if a == nil || a.Client == nil || probe == nil {
		return nil, fmt.Errorf("eval scenario runtime is not initialized")
	}
	switch item.Scenario.Kind {
	case "", ScenarioNormal:
		return noScenarioCleanup, nil
	case ScenarioApprovalDecision:
		approval, err := probe.WaitForApproval(ctx, handle.RunID)
		if err != nil {
			return nil, err
		}
		if err := a.decideApprovalsUntilTerminal(ctx, item, handle, probe, approval); err != nil {
			return nil, err
		}
		return noScenarioCleanup, nil
	case ScenarioCheckpointResume:
		attempt, err := probe.WaitForCheckpoint(ctx, handle.RunID)
		if err != nil {
			return nil, err
		}
		if item.Scenario.Decision != "" {
			approval, approvalErr := probe.WaitForApproval(ctx, handle.RunID)
			if approvalErr != nil {
				return nil, approvalErr
			}
			if err := a.decideApprovalsUntilTerminal(ctx, item, handle, probe, approval); err != nil {
				return nil, err
			}
			// 审批类 Checkpoint 恢复由 Worker 池完成：Approval 发布后 Worker
			// 已释放租约，无需（也不应）挂起仍持有租约的发布中 Worker。
			return noScenarioCleanup, nil
		}
		if a.Faults == nil {
			return nil, fmt.Errorf("checkpoint resume requires an explicit local fault controller")
		}
		restore, err := a.Faults.SuspendWorker(ctx, attempt)
		if err != nil {
			return nil, err
		}
		return nonNilCleanup(restore), nil
	case ScenarioPreCheckpointReplay:
		if a.Faults == nil {
			return nil, fmt.Errorf("pre-checkpoint replay requires an explicit local fault controller")
		}
		attempt, err := probe.WaitForRunningWithoutCheckpoint(ctx, handle.RunID)
		if err != nil {
			return nil, err
		}
		return a.Faults.SuspendWorker(ctx, attempt)
	case ScenarioDependencyParked:
		if a.Faults == nil {
			return nil, fmt.Errorf("dependency parked requires an explicit local fault controller")
		}
		attempt, err := probe.WaitForDependencyCall(ctx, handle.RunID, item.Scenario.Dependency)
		if err != nil {
			return nil, err
		}
		return a.Faults.SuspendDependency(ctx, item.Scenario.Dependency, attempt)
	default:
		return nil, fmt.Errorf("unsupported eval scenario %q", item.Scenario.Kind)
	}
}

// decideApprovalsUntilTerminal 同一 approver 决策首个及后续合法提案（例如
// block_ip 的 derived nginx 步骤）；Run 到达终态时 WaitForApproval 返回错误
// 即停止，整体受 Eval 调用方 ctx 超时约束；同一提案 ID 去重避免重复决策。
func (a *HTTPRuntimeAdapter) decideApprovalsUntilTerminal(
	ctx context.Context,
	item EvalCase,
	handle RunHandle,
	probe ScenarioProbe,
	first ApprovalTruth,
) error {
	decided := map[string]bool{first.ID: true}
	if err := a.decideApproval(ctx, item.Scenario, first); err != nil {
		return err
	}
	for {
		next, waitErr := probe.WaitForApproval(ctx, handle.RunID)
		if waitErr != nil {
			return nil
		}
		if decided[next.ID] {
			// 同一提案已决策（探测桩或提交竞态），视为没有新提案。
			return nil
		}
		if decideErr := a.decideApproval(ctx, item.Scenario, next); decideErr != nil {
			return decideErr
		}
		decided[next.ID] = true
	}
}

func (a *HTTPRuntimeAdapter) decideApproval(ctx context.Context, scenario Scenario, observed ApprovalTruth) error {
	header, err := a.headerFor(scenario.DecisionIdentity)
	if err != nil {
		return err
	}
	subject, role, err := bearerIdentity(header)
	if err != nil {
		return err
	}
	if ExecutionIdentity(role) != scenario.DecisionIdentity {
		return fmt.Errorf("approval decision token role does not match the configured eval identity")
	}
	if subject == observed.RequestedBy {
		return fmt.Errorf("approval proposer cannot decide its own proposal")
	}
	current, err := a.getApproval(ctx, header, observed.ID)
	if err != nil {
		return err
	}
	if current.ID != observed.ID || current.RunID != observed.RunID || current.ProposalHash != observed.ProposalHash || current.Version != observed.Version || current.Status != "pending" {
		return fmt.Errorf("public Approval API truth does not match the observed pending Approval")
	}
	payload, err := json.Marshal(approvalDecisionRequest{
		ProposalHash: observed.ProposalHash, Version: observed.Version, Reason: "P43 isolated runtime evaluation",
	})
	if err != nil {
		return fmt.Errorf("marshal Approval decision: %w", err)
	}
	path := "/api/ops/v1/approvals/" + url.PathEscape(observed.ID) + "/" + scenario.Decision
	response, err := a.do(ctx, http.MethodPost, path, header, bytes.NewReader(payload), "application/json")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("production Approval decision API returned HTTP %d", response.StatusCode)
	}
	return nil
}

func (a *HTTPRuntimeAdapter) getApproval(ctx context.Context, header http.Header, approvalID string) (approvalAPIItem, error) {
	path := "/api/ops/v1/approvals/" + url.PathEscape(approvalID)
	response, err := a.do(ctx, http.MethodGet, path, header, nil, "")
	if err != nil {
		return approvalAPIItem{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return approvalAPIItem{}, fmt.Errorf("production Approval query API returned HTTP %d", response.StatusCode)
	}
	var envelope approvalAPIEnvelope
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&envelope); err != nil {
		return approvalAPIItem{}, fmt.Errorf("decode production Approval response: %w", err)
	}
	if envelope.Data == nil || envelope.Data.Item.ID == "" {
		return approvalAPIItem{}, fmt.Errorf("production Approval response has no item")
	}
	return envelope.Data.Item, nil
}

func (a *HTTPRuntimeAdapter) do(
	ctx context.Context,
	method, path string,
	header http.Header,
	body io.Reader,
	contentType string,
) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, a.BaseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("build production API request: %w", err)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	for key, values := range header {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	response, err := a.Client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("call production API: %w", err)
	}
	return response, nil
}

func bearerIdentity(header http.Header) (string, string, error) {
	parts := strings.SplitN(strings.TrimSpace(header.Get("Authorization")), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", "", fmt.Errorf("approval decision identity requires a bearer token")
	}
	tokenParts := strings.Split(parts[1], ".")
	if len(tokenParts) != 3 {
		return "", "", fmt.Errorf("approval decision identity token is malformed")
	}
	payload, err := base64.RawURLEncoding.DecodeString(tokenParts[1])
	if err != nil {
		return "", "", fmt.Errorf("approval decision identity token is malformed")
	}
	defer clear(payload)
	var claims struct {
		UserID string `json:"uid"`
		Role   string `json:"role"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || strings.TrimSpace(claims.UserID) == "" || strings.TrimSpace(claims.Role) == "" {
		return "", "", fmt.Errorf("approval decision identity token claims are invalid")
	}
	return claims.UserID, claims.Role, nil
}

func noScenarioCleanup() error { return nil }

func nonNilCleanup(cleanup func() error) func() error {
	if cleanup == nil {
		return noScenarioCleanup
	}
	return cleanup
}

func restoreAfterError(restore func() error, cause error) error {
	if restore == nil {
		return cause
	}
	if err := restore(); err != nil {
		return fmt.Errorf("%v; restore local scenario process: %w", cause, err)
	}
	return cause
}
