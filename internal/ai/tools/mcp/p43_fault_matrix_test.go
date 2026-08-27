//go:build p43_fault_matrix

package mcp

import (
	"context"
	"errors"
	"testing"

	"SentinelOps/internal/testutil/faultmatrix"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestP43FaultMatrixMCPCases 验证初始化和调用断线均走官方 Session 且不重试未知调用。
func TestP43FaultMatrixMCPCases(t *testing.T) {
	matrix, err := faultmatrix.Load("../../../../manifest/ci/fault-matrix.yaml")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("mcp_init_disconnect", func(t *testing.T) {
		p43RequireOutcome(t, matrix, "mcp_init_disconnect", faultmatrix.Parked)
		TestMCPOpenToolsListFailureClosesImmediately(t)
	})
	t.Run("mcp_call_disconnect", func(t *testing.T) {
		p43RequireOutcome(t, matrix, "mcp_call_disconnect", faultmatrix.Parked)
		callErr := errors.New("transport closed before tool result")
		fake := &p43FailingCallSession{err: callErr}
		owner := &SessionOwner{
			config: ServerConfig{Name: "required", Enabled: true, Required: true, ConnectAttempts: 1},
			connect: func(context.Context, ServerConfig) (ClientSession, func() error, error) {
				return fake, func() error { fake.closed = true; return nil }, nil
			},
		}
		session, err := owner.Open(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = session.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: "read_only"}); !errors.Is(err, callErr) {
			t.Fatalf("call error=%v want=%v", err, callErr)
		}
		if fake.calls != 1 {
			t.Fatalf("unknown MCP call was retried %d times", fake.calls)
		}
		if err = owner.Close(); err != nil || !fake.closed {
			t.Fatalf("close=%v closed=%t", err, fake.closed)
		}
	})
}

type p43FailingCallSession struct {
	err    error
	calls  int
	closed bool
}

func (*p43FailingCallSession) ListTools(context.Context, *sdkmcp.ListToolsParams) (*sdkmcp.ListToolsResult, error) {
	return &sdkmcp.ListToolsResult{}, nil
}

func (s *p43FailingCallSession) CallTool(context.Context, *sdkmcp.CallToolParams) (*sdkmcp.CallToolResult, error) {
	s.calls++
	return nil, s.err
}

func p43RequireOutcome(t *testing.T, matrix faultmatrix.Matrix, id, want string) {
	t.Helper()
	entry, ok := matrix.Case(id)
	if !ok {
		t.Fatalf("fault matrix does not define %q", id)
	}
	if entry.ExpectedOutcome != want {
		t.Fatalf("outcome=%q want=%q", entry.ExpectedOutcome, want)
	}
}
