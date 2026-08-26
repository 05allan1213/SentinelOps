package effects

import (
	"context"
	"errors"
	"testing"
)

func TestShadowModeApprovedEffectFailsBeforeEndpoint(t *testing.T) {
	called := false
	request := TransactionalRequest{
		GateAllowed: true,
		GateCheck: func(context.Context) (bool, error) {
			return false, nil
		},
	}
	err := callEffectEndpoint(context.Background(), request, func(context.Context) (string, error) {
		called = true
		return "mutated", nil
	})
	if !errors.Is(err, ErrEffectGateClosed) || called {
		t.Fatalf("closed Gate err=%v endpoint_called=%t", err, called)
	}
}

func TestEffectiveGateCheckErrorFailsBeforeEndpoint(t *testing.T) {
	called := false
	want := errors.New("settings unavailable")
	err := callEffectEndpoint(context.Background(), TransactionalRequest{
		GateAllowed: true,
		GateCheck:   func(context.Context) (bool, error) { return false, want },
	}, func(context.Context) (string, error) {
		called = true
		return "mutated", nil
	})
	if !errors.Is(err, want) || called {
		t.Fatalf("Gate read err=%v endpoint_called=%t", err, called)
	}
}
