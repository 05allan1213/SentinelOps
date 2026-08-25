package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type budgetTool struct {
	base       tool.BaseTool
	invokable  tool.InvokableTool
	serverName string
	toolName   string
	budget     BudgetHook
	identity   func(context.Context, string, string) string
	maxBytes   int
	maxChars   int
	timeout    time.Duration
}

func (t *budgetTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.base.Info(ctx)
}

func (t *budgetTool) InvokableRun(ctx context.Context, args string, opts ...tool.Option) (string, error) {
	if t.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, t.timeout)
		defer cancel()
	}
	identity := t.serverName + ":" + t.toolName
	if t.identity != nil {
		identity = t.identity(ctx, t.toolName, args)
	}
	if t.budget != nil {
		if err := t.budget.ReserveMCP(ctx, BudgetRequest{ReservationIdentity: identity, Subject: t.toolName, Calls: 1, Concurrency: 1, ResultChars: int64(t.maxChars), ResultBytes: int64(t.maxBytes)}); err != nil {
			return "", err
		}
	}
	result, err := t.invokable.InvokableRun(ctx, args, opts...)
	chars, bytes := int64(len([]rune(result))), int64(len(result))
	if t.maxBytes > 0 && int(bytes) > t.maxBytes {
		if t.budget != nil {
			_ = t.budget.SettleMCP(ctx, BudgetSettlement{ReservationIdentity: identity, Succeeded: false, ResultChars: chars, ResultBytes: bytes})
		}
		return "", fmt.Errorf("mcp result exceeds byte limit: %d > %d", bytes, t.maxBytes)
	}
	if t.budget != nil {
		if settleErr := t.budget.SettleMCP(ctx, BudgetSettlement{ReservationIdentity: identity, Succeeded: err == nil, ResultChars: chars, ResultBytes: bytes}); settleErr != nil && err == nil {
			err = settleErr
		}
	}
	return result, err
}

func defaultReservationIdentity(_ context.Context, server, toolName, args string) string {
	hash := sha256.Sum256([]byte(args))
	return server + ":" + toolName + ":" + hex.EncodeToString(hash[:])
}
