package mysql

import (
	"fmt"
	"testing"
	"time"
)

func TestActiveSessionUniqueIndexRetainsAllNonTerminalStates(t *testing.T) {
	_, db, dsn := newDisposableDatabase(t, "p07_active_session")
	requireMigrationsUp(t, dsn)

	statuses := []string{"pending", "running", "waiting_approval", "retryable_failed", "parked", "reconciling"}
	for index, status := range statuses {
		sessionKey := fmt.Sprintf("active-session-%d", index)
		first := WorkflowRun{
			ID:               fmt.Sprintf("first-%d", index),
			WorkflowKey:      "test",
			Status:           status,
			RuntimeMode:      "legacy",
			ActiveSessionKey: &sessionKey,
			StartedAt:        time.Now(),
		}
		if err := db.Create(&first).Error; err != nil {
			t.Fatalf("create %s occupant: %v", status, err)
		}
		conflict := WorkflowRun{
			ID:               fmt.Sprintf("conflict-%d", index),
			WorkflowKey:      "test",
			Status:           "running",
			RuntimeMode:      "legacy",
			ActiveSessionKey: &sessionKey,
			StartedAt:        time.Now(),
		}
		if err := db.Create(&conflict).Error; err == nil {
			t.Fatalf("status %s did not retain unique active_session_key", status)
		}

		if err := db.Model(&WorkflowRun{}).Where("id = ?", first.ID).Updates(map[string]any{
			"status":             "failed",
			"active_session_key": nil,
		}).Error; err != nil {
			t.Fatalf("release terminal session for %s: %v", status, err)
		}
		if err := db.Create(&conflict).Error; err != nil {
			t.Fatalf("reuse released session after %s terminal: %v", status, err)
		}
	}
}
