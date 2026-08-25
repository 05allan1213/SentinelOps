package breaker

import (
	"errors"
	"testing"
	"time"
)

func TestBreakerSharedByCatalogRef(t *testing.T) {
	registry := NewRegistry(Settings{FailureThreshold: 2, OpenTimeout: time.Hour})
	first, err := registry.For("provider_a/shared")
	if err != nil {
		t.Fatal(err)
	}
	second, err := registry.For("provider_a/shared")
	if err != nil {
		t.Fatal(err)
	}
	other, err := registry.For("provider_b/shared")
	if err != nil {
		t.Fatal(err)
	}
	first.Failure()
	second.Failure()
	if err = first.Allow(); !errors.Is(err, ErrOpen) {
		t.Fatalf("shared breaker error = %v, want ErrOpen", err)
	}
	if err = other.Allow(); err != nil {
		t.Fatalf("other Catalog Ref breaker leaked state: %v", err)
	}
}

func TestBreakerSuccessResetsConsecutiveFailures(t *testing.T) {
	registry := NewRegistry(Settings{FailureThreshold: 2, OpenTimeout: time.Hour})
	health, err := registry.For("provider_a/chat")
	if err != nil {
		t.Fatal(err)
	}
	health.Failure()
	health.Success()
	health.Failure()
	if err = health.Allow(); err != nil {
		t.Fatalf("breaker opened without consecutive failures: %v", err)
	}
}
