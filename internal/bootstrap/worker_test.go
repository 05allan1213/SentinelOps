package bootstrap

import (
	"strings"
	"testing"
)

func TestDurableWorkerOwnerUsesExplicitProcessIdentity(t *testing.T) {
	t.Setenv(workerIDEnv, "p43-worker-a")
	owner, err := durableWorkerOwner()
	if err != nil {
		t.Fatalf("durableWorkerOwner() error = %v", err)
	}
	if owner != "p43-worker-a" {
		t.Fatalf("durableWorkerOwner() = %q", owner)
	}
}

func TestDurableWorkerOwnerRejectsAmbiguousIdentity(t *testing.T) {
	for _, owner := range []string{"", " padded", strings.Repeat("a", 129)} {
		t.Run(owner, func(t *testing.T) {
			t.Setenv(workerIDEnv, owner)
			if _, err := durableWorkerOwner(); err == nil {
				t.Fatalf("durableWorkerOwner() accepted %q", owner)
			}
		})
	}
}
