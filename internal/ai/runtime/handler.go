package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"SentinelOps/internal/ai/effects"
	"SentinelOps/internal/ai/limiter"
	"SentinelOps/internal/ai/policy"
	aitrace "SentinelOps/internal/ai/trace"
	"SentinelOps/internal/ai/workflow"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

const toolReservationDomain = "sentinelops/tool-call-reservation/v1\x00"

type modelInvocationContextKey struct{}
type callMetadataContextKey struct{}

// CallMetadata 是 RuntimeHandler 传给原 endpoint 与 Trace Callback 的只读安全事实。
type CallMetadata struct {
	RunID               string
	UserID              string
	Attempt             uint
	LeaseGeneration     uint64
	TraceID             string
	Deadline            time.Time
	ReservationIdentity string
	Kind                BudgetCallKind
	Subject             string
	Model               *ModelInvocation
}

// RuntimeHandler 直接实现 Eino ChatModelAgentMiddleware 的五个调用挂载点。
// 它只嵌入无状态官方基类，不保存任何请求可变状态。
type RuntimeHandler struct {
	*adk.BaseChatModelAgentMiddleware
	approvalStore *workflow.GORMStore
	effects       *effects.Executor
	modelDisabled bool
}

// ToolOnly returns a stateless view that keeps tool approval/effect behavior
// while leaving model wrapping to the physically bound candidate wrapper.
// This is required for Eino agents whose middleware receives an opaque
// endpoint wrapper and therefore cannot preserve per-candidate identity.
func (h *RuntimeHandler) ToolOnly() *RuntimeHandler {
	if h == nil {
		return nil
	}
	copy := *h
	copy.modelDisabled = true
	return &copy
}

var _ adk.ChatModelAgentMiddleware = (*RuntimeHandler)(nil)

// NewRuntimeHandler 创建 P14/P22/P28 继续原位扩展的唯一共享 Handler。
func NewRuntimeHandler() *RuntimeHandler {
	return &RuntimeHandler{BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{}}
}

// NewHITLRuntimeHandler 原位启用 Approval/Effect 生命周期；P26 生产 Worker 复用它，写 Gate 继续关闭。
func NewHITLRuntimeHandler(store *workflow.GORMStore) (*RuntimeHandler, error) {
	if store == nil {
		return nil, fmt.Errorf("workflow GORMStore is required for HITL")
	}
	executor, err := effects.NewExecutor(store)
	if err != nil {
		return nil, err
	}
	return &RuntimeHandler{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		approvalStore:                store,
		effects:                      executor,
	}, nil
}

// RuntimeHandlerFirst 固定官方 Handlers 中第一个用户 Handler 为共享 RuntimeHandler。
// Eino v0.9.15 规定第一项是用户 wrapper 的最外层；后续 builder 只能消费本函数结果。
func RuntimeHandlerFirst(handler *RuntimeHandler, additional ...adk.ChatModelAgentMiddleware) ([]adk.ChatModelAgentMiddleware, error) {
	if handler == nil {
		return nil, fmt.Errorf("runtime Handler is required")
	}
	result := make([]adk.ChatModelAgentMiddleware, 0, len(additional)+1)
	result = append(result, handler)
	result = append(result, additional...)
	return result, nil
}

// WithModelInvocation 将单次 physical Model endpoint 的 provider-qualified 身份写入 ctx。
// 不允许使用进程全局 current model；Retry/Failover 后续必须为每个 physical call 传入独立 identity。
func WithModelInvocation(ctx context.Context, invocation ModelInvocation) (context.Context, error) {
	if ctx == nil {
		return nil, fmt.Errorf("model invocation context is required")
	}
	for name, value := range map[string]string{
		"reservation_identity": invocation.ReservationIdentity, "catalog_ref": invocation.CatalogRef,
		"provider": invocation.Provider, "driver": invocation.Driver, "model_id": invocation.ModelID,
		"profile": invocation.Profile, "snapshot_identity": invocation.SnapshotIdentity,
	} {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("model invocation %s is required", name)
		}
		if policy.NewRedactor().RedactText(value) != value {
			return nil, fmt.Errorf("model invocation %s contains sensitive material", name)
		}
	}
	return context.WithValue(ctx, modelInvocationContextKey{}, invocation), nil
}

// CallMetadataFromContext 读取 RuntimeHandler 在所有安全检查与 reserve 后注入的 metadata。
func CallMetadataFromContext(ctx context.Context) (CallMetadata, error) {
	if ctx == nil {
		return CallMetadata{}, fmt.Errorf("runtime Handler call metadata is missing")
	}
	metadata, ok := ctx.Value(callMetadataContextKey{}).(CallMetadata)
	if !ok || metadata.RunID == "" || metadata.ReservationIdentity == "" {
		return CallMetadata{}, fmt.Errorf("runtime Handler call metadata is missing")
	}
	metadata.Model = cloneModelInvocation(metadata.Model)
	return metadata, nil
}

// WrapModel 在官方真实 Model endpoint 外执行 limiter、durable reserve/settle 与 metadata 注入。
func (h *RuntimeHandler) WrapModel(_ context.Context, endpoint model.BaseChatModel, _ *adk.ModelContext) (model.BaseChatModel, error) {
	if h == nil || endpoint == nil {
		return nil, fmt.Errorf("runtime Handler and Model endpoint are required")
	}
	if h.modelDisabled {
		return endpoint, nil
	}
	if endpointType, ok := components.GetType(endpoint); ok && endpointType == "FailoverProxyModel" {
		// P27 已把同一 Handler 绑定到每个真实候选；代理层不能再次 reserve/settle。
		return endpoint, nil
	}
	if _, ok := endpoint.(interface{ DurablePhysicalModel() }); ok {
		// Physical candidates already inject invocation identity and perform the
		// durable reserve/settle around the endpoint call.
		return endpoint, nil
	}
	return &runtimeModelEndpoint{handler: h, endpoint: endpoint}, nil
}

// WrapInvokableToolCall 保护普通同步 Tool endpoint。
func (h *RuntimeHandler) WrapInvokableToolCall(_ context.Context, endpoint adk.InvokableToolCallEndpoint, toolContext *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	if h == nil || endpoint == nil {
		return nil, fmt.Errorf("runtime Handler and invokable Tool endpoint are required")
	}
	return func(ctx context.Context, arguments string, options ...tool.Option) (string, error) {
		approved, handled, approvalErr := h.handleApprovalToolCall(ctx, toolContext, arguments)
		if handled {
			if approvalErr != nil {
				return "", approvalErr
			}
			result, err := h.effects.Execute(ctx, approved.Request, func(callbackCtx context.Context) (string, error) {
				return endpoint(callbackCtx, approved.Request.ArgumentsJSON, options...)
			})
			return result.Response, err
		}
		callContext, budget, reservation, err := h.prepareToolCall(ctx, toolContext)
		if err != nil {
			return "", err
		}
		result, endpointErr := endpoint(callContext, arguments, options...)
		actual, quality := toolBudgetActual(result)
		var settleErr error
		if !isInterruptError(endpointErr) {
			settleErr = settleRuntimeCallWithUsage(callContext, budget, reservation, endpointErr == nil, actual, quality)
		}
		return result, errors.Join(endpointErr, settleErr)
	}, nil
}

// WrapStreamableToolCall 保护普通流式 Tool endpoint；完整消费、错误或提前关闭时才结算。
func (h *RuntimeHandler) WrapStreamableToolCall(_ context.Context, endpoint adk.StreamableToolCallEndpoint, toolContext *adk.ToolContext) (adk.StreamableToolCallEndpoint, error) {
	if h == nil || endpoint == nil {
		return nil, fmt.Errorf("runtime Handler and streamable Tool endpoint are required")
	}
	return func(ctx context.Context, arguments string, options ...tool.Option) (*schema.StreamReader[string], error) {
		approved, handled, approvalErr := h.handleApprovalToolCall(ctx, toolContext, arguments)
		if handled {
			if approvalErr != nil {
				return nil, approvalErr
			}
			_ = approved
			return nil, ErrTransactionalEffectEndpointUnsupported
		}
		callContext, budget, reservation, err := h.prepareToolCall(ctx, toolContext)
		if err != nil {
			return nil, err
		}
		result, endpointErr := endpoint(callContext, arguments, options...)
		if endpointErr != nil {
			if isInterruptError(endpointErr) {
				return nil, endpointErr
			}
			return nil, errors.Join(endpointErr, settleRuntimeCall(callContext, budget, reservation, false))
		}
		if result == nil {
			return nil, errors.Join(fmt.Errorf("streamable Tool endpoint returned a nil stream"), settleRuntimeCall(callContext, budget, reservation, false))
		}
		return settlingStreamWithUsage(callContext, result, budget, reservation, func(value string) (*workflow.BaseBudgetActual, string) { return toolBudgetActual(value) }), nil
	}, nil
}

// WrapEnhancedInvokableToolCall 保护增强同步 Tool endpoint。
func (h *RuntimeHandler) WrapEnhancedInvokableToolCall(_ context.Context, endpoint adk.EnhancedInvokableToolCallEndpoint, toolContext *adk.ToolContext) (adk.EnhancedInvokableToolCallEndpoint, error) {
	if h == nil || endpoint == nil {
		return nil, fmt.Errorf("runtime Handler and enhanced Tool endpoint are required")
	}
	return func(ctx context.Context, argument *schema.ToolArgument, options ...tool.Option) (*schema.ToolResult, error) {
		rawArguments := ""
		if argument != nil {
			rawArguments = argument.Text
		}
		approved, handled, approvalErr := h.handleApprovalToolCall(ctx, toolContext, rawArguments)
		if handled {
			if approvalErr != nil {
				return nil, approvalErr
			}
			_ = approved
			return nil, ErrTransactionalEffectEndpointUnsupported
		}
		callContext, budget, reservation, err := h.prepareToolCall(ctx, toolContext)
		if err != nil {
			return nil, err
		}
		result, endpointErr := endpoint(callContext, argument, options...)
		actual, quality := toolResultBudgetActual(result)
		var settleErr error
		if !isInterruptError(endpointErr) {
			settleErr = settleRuntimeCallWithUsage(callContext, budget, reservation, endpointErr == nil, actual, quality)
		}
		return result, errors.Join(endpointErr, settleErr)
	}, nil
}

// WrapEnhancedStreamableToolCall 保护增强流式 Tool endpoint。
func (h *RuntimeHandler) WrapEnhancedStreamableToolCall(_ context.Context, endpoint adk.EnhancedStreamableToolCallEndpoint, toolContext *adk.ToolContext) (adk.EnhancedStreamableToolCallEndpoint, error) {
	if h == nil || endpoint == nil {
		return nil, fmt.Errorf("runtime Handler and enhanced streamable Tool endpoint are required")
	}
	return func(ctx context.Context, argument *schema.ToolArgument, options ...tool.Option) (*schema.StreamReader[*schema.ToolResult], error) {
		rawArguments := ""
		if argument != nil {
			rawArguments = argument.Text
		}
		approved, handled, approvalErr := h.handleApprovalToolCall(ctx, toolContext, rawArguments)
		if handled {
			if approvalErr != nil {
				return nil, approvalErr
			}
			_ = approved
			return nil, ErrTransactionalEffectEndpointUnsupported
		}
		callContext, budget, reservation, err := h.prepareToolCall(ctx, toolContext)
		if err != nil {
			return nil, err
		}
		result, endpointErr := endpoint(callContext, argument, options...)
		if endpointErr != nil {
			if isInterruptError(endpointErr) {
				return nil, endpointErr
			}
			return nil, errors.Join(endpointErr, settleRuntimeCall(callContext, budget, reservation, false))
		}
		if result == nil {
			return nil, errors.Join(fmt.Errorf("enhanced streamable Tool endpoint returned a nil stream"), settleRuntimeCall(callContext, budget, reservation, false))
		}
		return settlingStreamWithUsage(callContext, result, budget, reservation, toolResultBudgetActual), nil
	}, nil
}

type runtimeModelEndpoint struct {
	handler  *RuntimeHandler
	endpoint model.BaseChatModel
}

func (m *runtimeModelEndpoint) Generate(ctx context.Context, input []*schema.Message, options ...model.Option) (*schema.Message, error) {
	callContext, budget, reservation, err := m.handler.prepareModelCall(ctx, input)
	if err != nil {
		return nil, err
	}
	result, endpointErr := m.endpoint.Generate(callContext, input, options...)
	actual, quality := modelBudgetActual(result)
	settleErr := settleRuntimeCallWithUsage(callContext, budget, reservation, endpointErr == nil, actual, quality)
	if settleErr != nil {
		result = nil
	}
	return result, errors.Join(endpointErr, settleErr)
}

func (m *runtimeModelEndpoint) Stream(ctx context.Context, input []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	callContext, budget, reservation, err := m.handler.prepareModelCall(ctx, input)
	if err != nil {
		return nil, err
	}
	result, endpointErr := m.endpoint.Stream(callContext, input, options...)
	if endpointErr != nil {
		return nil, errors.Join(endpointErr, settleRuntimeCall(callContext, budget, reservation, false))
	}
	if result == nil {
		return nil, errors.Join(fmt.Errorf("model endpoint returned a nil stream"), settleRuntimeCall(callContext, budget, reservation, false))
	}
	return settlingStreamWithUsage(callContext, result, budget, reservation, modelBudgetActual), nil
}

func (h *RuntimeHandler) prepareModelCall(ctx context.Context, input ...[]*schema.Message) (context.Context, CallBudget, BudgetReservation, error) {
	attempt, budget, err := validateRuntimeCallContext(ctx)
	if err != nil {
		return nil, nil, BudgetReservation{}, err
	}
	invocation, ok := ctx.Value(modelInvocationContextKey{}).(ModelInvocation)
	if !ok {
		return nil, nil, BudgetReservation{}, fmt.Errorf("physical Model invocation identity is missing")
	}
	if err := validateModelInvocation(attempt.Snapshot, invocation); err != nil {
		return nil, nil, BudgetReservation{}, err
	}
	var estimateInput []*schema.Message
	if len(input) > 0 {
		estimateInput = input[0]
	}
	call := BudgetCall{
		ReservationIdentity: invocation.ReservationIdentity, Kind: BudgetCallKindModel,
		Subject: invocation.CatalogRef, Lease: attempt.Lease, TraceID: attempt.Trace.ID,
		Deadline: attempt.Deadline, Model: cloneModelInvocation(&invocation), Estimate: modelBudgetEstimate(estimateInput),
	}
	if err := limiter.Wait(ctx, invocation.CatalogRef); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			_, reserveErr := budget.ReserveCall(ctx, call)
			return nil, nil, BudgetReservation{}, errors.Join(err, reserveErr)
		}
		return nil, nil, BudgetReservation{}, err
	}
	reservation, err := budget.ReserveCall(ctx, call)
	if err != nil {
		return nil, nil, BudgetReservation{}, err
	}
	metadata := runtimeCallMetadata(attempt, reservation.Identity, BudgetCallKindModel, invocation.CatalogRef, &invocation)
	callCtx := context.WithValue(ctx, callMetadataContextKey{}, metadata)
	callCtx = aitrace.WithModelMetadata(callCtx, traceModelMetadata(attempt.Snapshot, invocation))
	return callCtx, budget, reservation, nil
}

func traceModelMetadata(snapshot FrozenRuntimeSnapshot, invocation ModelInvocation) aitrace.ModelMetadata {
	metadata := aitrace.ModelMetadata{
		Kind: "chat", CatalogRef: invocation.CatalogRef, Provider: invocation.Provider, Driver: invocation.Driver,
		ModelID: invocation.ModelID, Profile: invocation.Profile, SnapshotIdentity: invocation.SnapshotIdentity,
		PricingRevision: invocation.PricingRevision, PricingCurrency: invocation.PricingCurrency, PricingUnit: invocation.PricingUnit,
		InputPrice: invocation.InputPrice, CachedInputPrice: invocation.CachedInputPrice, OutputPrice: invocation.OutputPrice,
	}
	for _, candidate := range snapshot.Models() {
		if candidate.Identity() != invocation.SnapshotIdentity {
			continue
		}
		metadata.Kind = candidate.Kind
		metadata.RouteOptions = map[string]any{"enable_thinking": candidate.RouteOptions.EnableThinking, "instruct": candidate.RouteOptions.Instruct}
		break
	}
	return metadata
}

func modelBudgetEstimate(input []*schema.Message) workflow.BaseBudgetEstimate {
	var chars int
	for _, message := range input {
		if message != nil {
			chars += len(message.Content)
		}
	}
	return workflow.BaseBudgetEstimate{InputTokens: int64((chars + 3) / 4)}
}

func toolBudgetActual(result string) (*workflow.BaseBudgetActual, string) {
	return &workflow.BaseBudgetActual{ResultChars: int64(len([]rune(result))), ResultBytes: int64(len(result))}, "reliable"
}

func toolResultBudgetActual(result *schema.ToolResult) (*workflow.BaseBudgetActual, string) {
	if result == nil {
		return nil, "unknown"
	}
	var text string
	for _, part := range result.Parts {
		text += part.Text
	}
	return toolBudgetActual(text)
}

func (h *RuntimeHandler) prepareToolCall(ctx context.Context, toolContext *adk.ToolContext) (context.Context, CallBudget, BudgetReservation, error) {
	attempt, budget, err := validateRuntimeCallContext(ctx)
	if err != nil {
		return nil, nil, BudgetReservation{}, err
	}
	if toolContext == nil || strings.TrimSpace(toolContext.Name) == "" || strings.TrimSpace(toolContext.CallID) == "" {
		return nil, nil, BudgetReservation{}, fmt.Errorf("tool name and call ID are required")
	}
	dynamicCatalog, hasDynamicCatalog := DynamicToolCatalogFromContext(ctx)
	dynamicEntry, isDynamic := dynamicCatalog.Lookup(toolContext.Name)
	if err := policy.RequireExecutable(toolContext.Name); err != nil {
		if !isDynamic {
			return nil, nil, BudgetReservation{}, err
		}
		if !dynamicEntry.ReadOnly {
			return nil, nil, BudgetReservation{}, policy.ErrMutationDisabled
		}
	}
	if err := validateToolSnapshot(attempt.Snapshot, toolContext.Name); err != nil {
		if !hasDynamicCatalog || !isDynamic || dynamicCatalog.Hash == "" || dynamicCatalog.Hash != attempt.Snapshot.document.MCPCatalogHash {
			return nil, nil, BudgetReservation{}, err
		}
	}
	reservationIdentity := toolReservationIdentity(attempt.Run.ID, toolContext.Name, toolContext.CallID)
	callKind := BudgetCallKindL0Tool
	if isDynamic {
		callKind = BudgetCallKindMCP
	}
	call := BudgetCall{
		ReservationIdentity: reservationIdentity, Kind: callKind,
		Subject: toolContext.Name, Lease: attempt.Lease, TraceID: attempt.Trace.ID, Deadline: attempt.Deadline,
	}
	reservation, err := budget.ReserveCall(ctx, call)
	if err != nil {
		return nil, nil, BudgetReservation{}, err
	}
	metadata := runtimeCallMetadata(attempt, reservation.Identity, callKind, toolContext.Name, nil)
	return context.WithValue(ctx, callMetadataContextKey{}, metadata), budget, reservation, nil
}

func validateRuntimeCallContext(ctx context.Context) (*AttemptContext, CallBudget, error) {
	if ctx == nil {
		return nil, nil, ErrAttemptContextMissing
	}
	if err := ctx.Err(); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return nil, nil, err
	}
	attempt, err := AttemptContextFromContext(ctx)
	if err != nil {
		return nil, nil, err
	}
	identity, err := policy.IdentityFromContext(ctx)
	if err != nil {
		return nil, nil, err
	}
	if identity != attempt.Identity || identity.Scope != attempt.Scope || attempt.Run.ID == "" || attempt.Run.Attempt == 0 ||
		attempt.Trace.ID == "" || attempt.Run.LeaseGeneration == 0 || attempt.Deadline.IsZero() {
		return nil, nil, fmt.Errorf("typed Runtime identity or Scope mismatch: %w", policy.ErrForbidden)
	}
	lease, err := workflow.LeaseTokenFromContext(ctx)
	if err != nil {
		return nil, nil, err
	}
	if lease != attempt.Lease || lease.RunID != attempt.Run.ID || lease.Generation != attempt.Run.LeaseGeneration {
		return nil, nil, workflow.ErrLeaseLost
	}
	budget, err := requireCallBudget(attempt.Budget)
	if err != nil {
		return nil, nil, err
	}
	return attempt, budget, nil
}

func validateModelInvocation(snapshot FrozenRuntimeSnapshot, invocation ModelInvocation) error {
	for _, candidate := range snapshot.Models() {
		if candidate.Kind == "chat" && candidate.CatalogRef == invocation.CatalogRef && candidate.Provider == invocation.Provider &&
			candidate.Driver == invocation.Driver && candidate.ModelID == invocation.ModelID && candidate.Profile == invocation.Profile &&
			candidate.Identity() == invocation.SnapshotIdentity {
			return nil
		}
	}
	return fmt.Errorf("physical Model invocation does not match Frozen Runtime Snapshot: %w", policy.ErrForbidden)
}

func validateToolSnapshot(snapshot FrozenRuntimeSnapshot, toolName string) error {
	entry, err := policy.LookupCatalog(toolName)
	if err != nil {
		return err
	}
	for _, frozen := range snapshot.Tools() {
		if frozen.Name == toolName && frozen.Revision == entry.Revision && frozen.SchemaHash == entry.SchemaHash {
			return nil
		}
	}
	return fmt.Errorf("tool %q does not match Frozen Runtime Snapshot: %w", toolName, policy.ErrForbidden)
}

func settleRuntimeCall(ctx context.Context, budget CallBudget, reservation BudgetReservation, succeeded bool) error {
	return settleRuntimeCallWithUsage(ctx, budget, reservation, succeeded, nil, "unknown")
}

func settleRuntimeCallWithUsage(ctx context.Context, budget CallBudget, reservation BudgetReservation, succeeded bool, actual *workflow.BaseBudgetActual, quality string) error {
	metadata, err := CallMetadataFromContext(ctx)
	if err != nil {
		return err
	}
	if actual != nil && metadata.Model != nil && actual.CostCNY == 0 {
		uncached := actual.InputTokens - actual.CachedInputTokens
		if uncached < 0 {
			uncached = 0
		}
		actualCopy := *actual
		actualCopy.CostCNY = (float64(uncached)*metadata.Model.InputPrice + float64(actual.CachedInputTokens)*metadata.Model.CachedInputPrice + float64(actual.OutputTokens)*metadata.Model.OutputPrice) / 1_000_000
		actual = &actualCopy
	}
	lease, err := workflow.LeaseTokenFromContext(ctx)
	if err != nil {
		return err
	}
	return budget.SettleCall(ctx, BudgetSettlement{
		ReservationIdentity: reservation.Identity, Lease: lease,
		TraceID: metadata.TraceID, Succeeded: succeeded, Actual: actual, UsageQuality: quality,
	})
}

// isInterruptError keeps a durable tool reservation pending across an official
// Stateful/CompositeInterrupt. The same physical tool call is replayed on
// Resume and must settle only after its resumed endpoint returns.
func isInterruptError(err error) bool {
	if err == nil {
		return false
	}
	var signal *adk.InterruptSignal
	return errors.As(err, &signal)
}

func modelBudgetActual(message *schema.Message) (*workflow.BaseBudgetActual, string) {
	if message == nil || message.ResponseMeta == nil || message.ResponseMeta.Usage == nil {
		return nil, "unknown"
	}
	usage := message.ResponseMeta.Usage
	return &workflow.BaseBudgetActual{
		InputTokens: int64(usage.PromptTokens), CachedInputTokens: int64(usage.PromptTokenDetails.CachedTokens),
		OutputTokens: int64(usage.CompletionTokens), ReasoningTokens: int64(usage.CompletionTokensDetails.ReasoningTokens),
	}, "reliable"
}

func settlingStream[T any](ctx context.Context, source *schema.StreamReader[T], budget CallBudget, reservation BudgetReservation) *schema.StreamReader[T] {
	return settlingStreamWithUsage(ctx, source, budget, reservation, func(T) (*workflow.BaseBudgetActual, string) { return nil, "unknown" })
}

func settlingStreamWithUsage[T any](ctx context.Context, source *schema.StreamReader[T], budget CallBudget, reservation BudgetReservation, usage func(T) (*workflow.BaseBudgetActual, string)) *schema.StreamReader[T] {
	reader, writer := schema.Pipe[T](1)
	go func() {
		defer source.Close()
		defer writer.Close()
		var actual *workflow.BaseBudgetActual
		quality := "unknown"
		for {
			item, receiveErr := source.Recv()
			if errors.Is(receiveErr, io.EOF) {
				if settleErr := settleRuntimeCallWithUsage(ctx, budget, reservation, true, actual, quality); settleErr != nil {
					var zero T
					writer.Send(zero, settleErr)
				}
				return
			}
			if receiveErr != nil {
				var zero T
				if isInterruptError(receiveErr) {
					writer.Send(zero, receiveErr)
					return
				}
				writer.Send(zero, errors.Join(receiveErr, settleRuntimeCallWithUsage(ctx, budget, reservation, false, actual, quality)))
				return
			}
			if candidate, candidateQuality := usage(item); candidate != nil {
				actual, quality = candidate, candidateQuality
			}
			if writer.Send(item, nil) {
				_ = settleRuntimeCallWithUsage(ctx, budget, reservation, false, actual, quality)
				return
			}
		}
	}()
	return reader
}

func runtimeCallMetadata(attempt *AttemptContext, reservationIdentity string, kind BudgetCallKind, subject string, invocation *ModelInvocation) CallMetadata {
	return CallMetadata{
		RunID: attempt.Run.ID, UserID: attempt.Identity.UserID, Attempt: attempt.Run.Attempt,
		LeaseGeneration: attempt.Run.LeaseGeneration, TraceID: attempt.Trace.ID, Deadline: attempt.Deadline,
		ReservationIdentity: reservationIdentity, Kind: kind, Subject: subject, Model: cloneModelInvocation(invocation),
	}
}

func toolReservationIdentity(runID, toolName, callID string) string {
	digest := sha256.New()
	_, _ = digest.Write([]byte(toolReservationDomain))
	_, _ = digest.Write([]byte(runID))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(toolName))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(callID))
	return hex.EncodeToString(digest.Sum(nil))
}

func cloneModelInvocation(invocation *ModelInvocation) *ModelInvocation {
	if invocation == nil {
		return nil
	}
	cloned := *invocation
	return &cloned
}
