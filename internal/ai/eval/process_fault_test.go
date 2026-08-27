package eval

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestLocalProcessFaultControllerRejectsBroadOrUnknownTargets(t *testing.T) {
	for _, document := range []string{
		`{"workers":{"worker-a":{"pid":1,"command_contains":"server worker"}}}`,
		`{"workers":{"worker-a":{"pid":424242,"command_contains":"sh"}}}`,
		`{"workers":{},"unexpected":true}`,
	} {
		if _, err := LoadLocalProcessFaultController(strings.NewReader(document)); err == nil {
			t.Fatalf("process manifest was accepted: %s", document)
		}
	}
}

func TestLocalProcessFaultControllerSuspendsOnlyExplicitMatchingChild(t *testing.T) {
	command := exec.Command("sleep", "30")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
	})
	target := LocalProcessTarget{PID: command.Process.Pid, CommandContains: "sleep"}
	for attempt := 0; attempt < 100; attempt++ {
		if target.requireCommandMatch() == nil {
			break
		}
		if attempt == 99 {
			t.Fatal("child process command did not become observable")
		}
		time.Sleep(time.Millisecond)
	}
	controller := &LocalProcessFaultController{manifest: LocalProcessManifest{Workers: map[string]LocalProcessTarget{
		"worker-test": target,
	}}}
	restore, err := controller.SuspendWorker(t.Context(), AttemptTruth{LeaseOwner: "worker-test"})
	if err != nil {
		t.Fatalf("SuspendWorker() error = %v", err)
	}
	if err := restore(); err != nil {
		t.Fatalf("restore() error = %v", err)
	}
	if err := restore(); err != nil {
		t.Fatalf("idempotent restore() error = %v", err)
	}
}
