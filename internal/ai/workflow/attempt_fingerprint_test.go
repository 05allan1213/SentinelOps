package workflow

import (
	"strings"
	"testing"
)

func TestAttemptFingerprintIsDeterministicAndChangesWithIdentity(t *testing.T) {
	first := AttemptFingerprint("run-1", 1, 7)
	again := AttemptFingerprint("run-1", 1, 7)
	if first == "" || first != again {
		t.Fatalf("attempt fingerprint is not deterministic: first=%q again=%q", first, again)
	}
	if len(first) != 64 || strings.ToLower(first) != first {
		t.Fatalf("attempt fingerprint is not a lowercase sha256 digest: %q", first)
	}
	if AttemptFingerprint("run-2", 1, 7) == first {
		t.Fatal("different run produced the same attempt fingerprint")
	}
	if AttemptFingerprint("run-1", 2, 7) == first {
		t.Fatal("different attempt produced the same attempt fingerprint")
	}
	if AttemptFingerprint("run-1", 1, 8) == first {
		t.Fatal("different generation produced the same attempt fingerprint")
	}
}

func TestAttemptFingerprintDoesNotLeakRawIdentity(t *testing.T) {
	runID := "run-secret-identity"
	digest := AttemptFingerprint(runID, 3, 11)
	if strings.Contains(digest, runID) {
		t.Fatalf("attempt fingerprint leaked run identity: %q", digest)
	}
	if strings.Contains(digest, AttemptFingerprintDomain) {
		t.Fatalf("attempt fingerprint leaked its domain: %q", digest)
	}
}
