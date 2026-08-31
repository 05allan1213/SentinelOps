// Package runtime 提供 Runtime v1 服务层共享的授权与错误契约。
package runtime

import (
	"context"
	"errors"
	"strings"

	"SentinelOps/internal/ai/policy"
)

var (
	ErrRuntimeNotFound               = errors.New("runtime resource not found")
	ErrRuntimeForbidden              = errors.New("runtime access forbidden")
	ErrRuntimeInvalidFilter          = errors.New("runtime filter is invalid")
	ErrRuntimeContentExpansionDenied = errors.New("runtime content expansion denied")
	ErrRuntimeOperationConflict      = errors.New("runtime operation conflict")
	ErrRuntimeIdempotencyConflict    = errors.New("runtime idempotency conflict")
	ErrRuntimePrecondition           = errors.New("runtime precondition failed")
)

// ContentKind 是允许显式展开的 Runtime 内容闭集。
type ContentKind string

const (
	ContentKindHistory       ContentKind = "history"
	ContentKindEvidenceQuote ContentKind = "evidence_quote"
	ContentKindTracePrompt   ContentKind = "trace_prompt"
	ContentKindTraceResponse ContentKind = "trace_response"
	ContentKindToolArguments ContentKind = "tool_arguments"
)

const (
	ErrorCodeRuntimeNotFound            = "RUNTIME_NOT_FOUND"
	ErrorCodeRuntimeForbidden           = "RUNTIME_FORBIDDEN"
	ErrorCodeRuntimeInvalidFilter       = "RUNTIME_INVALID_FILTER"
	ErrorCodeRuntimeOperationConflict   = "RUNTIME_OPERATION_CONFLICT"
	ErrorCodeRuntimeIdempotencyConflict = "RUNTIME_IDEMPOTENCY_CONFLICT"
	ErrorCodeRuntimePreconditionFailed  = "RUNTIME_PRECONDITION_FAILED"
	ErrorCodeRuntimeInternal            = "RUNTIME_INTERNAL"
)

// ParseContentKind 在任何 DAO 查询前校验客户端 include 值。
func ParseContentKind(value string) (ContentKind, error) {
	kind := ContentKind(value)
	if kind.Valid() {
		return kind, nil
	}
	return "", ErrRuntimeInvalidFilter
}

// Valid 报告 content kind 是否属于冻结闭集。
func (kind ContentKind) Valid() bool {
	switch kind {
	case ContentKindHistory, ContentKindEvidenceQuote, ContentKindTracePrompt, ContentKindTraceResponse, ContentKindToolArguments:
		return true
	}
	return false
}

// AuthorizeRun 对已经加载出的 owner 执行 Runtime metadata 资源级授权。
func AuthorizeRun(ctx context.Context, ownerID string) error {
	if strings.TrimSpace(ownerID) == "" {
		return ErrRuntimeNotFound
	}
	if err := policy.Authorize(ctx, policy.PermissionViewScoped, policy.Resource{OwnerID: ownerID}); err != nil {
		return ErrRuntimeForbidden
	}
	return nil
}

// AuthorizeRuntimeContent 校验内容闭集，并对已经加载出的 owner 执行显式展开授权。
func AuthorizeRuntimeContent(ctx context.Context, ownerID string, contentKind ContentKind) error {
	if _, err := ParseContentKind(string(contentKind)); err != nil {
		return err
	}
	if strings.TrimSpace(ownerID) == "" {
		return ErrRuntimeNotFound
	}
	if err := policy.Authorize(ctx, policy.PermissionViewRuntimeContent, policy.Resource{OwnerID: ownerID}); err != nil {
		return ErrRuntimeContentExpansionDenied
	}
	return nil
}

// RequireRuntimeAdmin 拒绝客户端声明的 Scope，仅接受服务端 admin Identity。
func RequireRuntimeAdmin(ctx context.Context) error {
	if err := policy.Authorize(ctx, policy.PermissionRecoverRuntime, policy.Resource{}); err != nil {
		return ErrRuntimeForbidden
	}
	return nil
}

// RuntimeErrorCode 将服务层 sentinel error 映射为稳定、无敏感信息的控制器错误码。
func RuntimeErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrRuntimeNotFound):
		return ErrorCodeRuntimeNotFound
	case errors.Is(err, ErrRuntimeForbidden), errors.Is(err, ErrRuntimeContentExpansionDenied):
		return ErrorCodeRuntimeForbidden
	case errors.Is(err, ErrRuntimeInvalidFilter):
		return ErrorCodeRuntimeInvalidFilter
	case errors.Is(err, ErrRuntimeOperationConflict):
		return ErrorCodeRuntimeOperationConflict
	case errors.Is(err, ErrRuntimeIdempotencyConflict):
		return ErrorCodeRuntimeIdempotencyConflict
	case errors.Is(err, ErrRuntimePrecondition):
		return ErrorCodeRuntimePreconditionFailed
	default:
		return ErrorCodeRuntimeInternal
	}
}
