package runtime

import (
	"context"
	"testing"
	"time"

	mcptools "SentinelOps/internal/ai/tools/mcp"
	"SentinelOps/internal/ai/workflow"
)

type p32MCPBudgetDouble struct {
	reserved []BudgetCall
	settled  []BudgetSettlement
}

func (*p32MCPBudgetDouble) RuntimeBudgetHandle() {}
func (d *p32MCPBudgetDouble) ReserveCall(_ context.Context, call BudgetCall) (BudgetReservation, error) {
	d.reserved = append(d.reserved, call)
	return BudgetReservation{Identity: call.ReservationIdentity}, nil
}
func (d *p32MCPBudgetDouble) SettleCall(_ context.Context, settlement BudgetSettlement) error {
	d.settled = append(d.settled, settlement)
	return nil
}

func TestMCPBudgetHookUsesSharedReservationDimensions(t *testing.T) {
	double := &p32MCPBudgetDouble{}
	hook := &MCPBudgetHook{Attempt: &AttemptContext{Budget: double, Lease: workflow.LeaseToken{RunID: "run", Owner: "worker", Generation: 2}, Trace: TraceIdentity{ID: "trace"}, Deadline: time.Now().Add(time.Minute)}}
	if err := hook.ReserveMCP(context.Background(), mcptools.BudgetRequest{ReservationIdentity: "mcp-call", Subject: "inventory__search", Calls: 1, Concurrency: 1, ResultChars: 100, ResultBytes: 200}); err != nil {
		t.Fatal(err)
	}
	if err := hook.SettleMCP(context.Background(), mcptools.BudgetSettlement{ReservationIdentity: "mcp-call", Succeeded: true, ResultChars: 7, ResultBytes: 9}); err != nil {
		t.Fatal(err)
	}
	if len(double.reserved) != 1 || double.reserved[0].Kind != BudgetCallKindMCP || double.reserved[0].Estimate.ResultBytes != 200 {
		t.Fatalf("MCP reservation did not use shared dimensions: %#v", double.reserved)
	}
	if len(double.settled) != 1 || double.settled[0].Actual.ResultBytes != 9 {
		t.Fatalf("MCP settlement did not use shared dimensions: %#v", double.settled)
	}
}
