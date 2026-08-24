package workflow

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/dao/mysql"
)

func TestRevisionZeroConcurrentBootstrapUsesMySQLOnly(t *testing.T) {
	db := newP07Database(t, "revision_zero")
	preference := mysql.UserPreference{
		UserID:        "user-revision-zero",
		OutputStyle:   "concise",
		AnalysisDepth: "deep",
		FocusAreas:    `["supply_chain","web"]`,
		InferredNote:  "durable mysql preference",
	}
	if err := db.Create(&preference).Error; err != nil {
		t.Fatalf("create durable preference: %v", err)
	}

	const workers = 16
	store := NewGORMStore(db)
	ctx := policy.WithIdentity(context.Background(), policy.Identity{
		UserID: preference.UserID,
		Role:   policy.RoleViewer,
		Scope:  policy.Scope{UserID: preference.UserID},
	})
	revisions := make(chan mysql.SessionStateRevision, workers)
	errorsCh := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			revision, err := store.BootstrapRevisionZero(ctx, "session-revision-zero", preference.UserID)
			if err != nil {
				errorsCh <- err
				return
			}
			revisions <- *revision
		}()
	}
	wg.Wait()
	close(errorsCh)
	close(revisions)
	for err := range errorsCh {
		t.Errorf("concurrent bootstrap: %v", err)
	}

	var count int64
	if err := db.Model(&mysql.SessionStateRevision{}).
		Where("session_id = ? AND revision = 0", "session-revision-zero").
		Count(&count).Error; err != nil {
		t.Fatalf("count revision zero: %v", err)
	}
	if count != 1 {
		t.Fatalf("revision zero count = %d, want 1", count)
	}

	var first *mysql.SessionStateRevision
	for revision := range revisions {
		if revision.Revision != 0 {
			t.Errorf("revision = %d, want 0", revision.Revision)
		}
		if first == nil {
			copy := revision
			first = &copy
			continue
		}
		if revision.ID != first.ID || revision.StateJSON != first.StateJSON {
			t.Errorf("bootstrap returned different revision: first=%+v got=%+v", *first, revision)
		}
	}
	if first == nil {
		t.Fatal("bootstrap returned no revision")
	}

	var state struct {
		Schema     string `json:"schema"`
		Revision   uint64 `json:"revision"`
		Preference struct {
			OutputStyle   string   `json:"output_style"`
			AnalysisDepth string   `json:"analysis_depth"`
			FocusAreas    []string `json:"focus_areas"`
			InferredNote  string   `json:"inferred_note"`
		} `json:"preference"`
		Summary    string `json:"summary"`
		History    []any  `json:"history"`
		Provenance struct {
			Preference string `json:"preference"`
			History    string `json:"history"`
		} `json:"provenance"`
	}
	if err := json.Unmarshal([]byte(first.StateJSON), &state); err != nil {
		t.Fatalf("decode revision zero: %v", err)
	}
	if state.Schema != SessionStateSchemaV1 || state.Revision != 0 || state.Preference.OutputStyle != "concise" || state.Preference.AnalysisDepth != "deep" || state.Preference.InferredNote != "durable mysql preference" {
		t.Fatalf("revision zero durable state = %+v", state)
	}
	if len(state.Preference.FocusAreas) != 2 || state.Summary != "" || len(state.History) != 0 {
		t.Fatalf("revision zero fabricated history or lost preference: %+v", state)
	}
	if state.Provenance.Preference != "mysql.user_preferences" || state.Provenance.History != "empty_no_durable_mysql_history" {
		t.Fatalf("revision zero provenance = %+v", state.Provenance)
	}
}

func TestRevisionZeroHasNoRedisOrProcessMemorySource(t *testing.T) {
	source, err := os.ReadFile("session_revision.go")
	if err != nil {
		t.Fatalf("read Revision 0 source: %v", err)
	}
	for _, forbidden := range []string{"internal/dao/redis", "internal/ai/cache", "SessionMemory", "agent_trace", "TraceRun"} {
		if strings.Contains(string(source), forbidden) {
			t.Errorf("Revision 0 source contains forbidden migration truth %q", forbidden)
		}
	}
}

func TestRevisionZeroAllowsEmptyDurableHistoryAndPreference(t *testing.T) {
	db := newP07Database(t, "revision_zero_empty")
	ctx := policy.WithIdentity(context.Background(), policy.Identity{
		UserID: "empty-user",
		Role:   policy.RoleViewer,
		Scope:  policy.Scope{UserID: "empty-user"},
	})
	revision, err := NewGORMStore(db).BootstrapRevisionZero(ctx, "empty-session", "empty-user")
	if err != nil {
		t.Fatalf("bootstrap empty durable state: %v", err)
	}
	var state map[string]any
	if err := json.Unmarshal([]byte(revision.StateJSON), &state); err != nil {
		t.Fatalf("decode empty revision zero: %v", err)
	}
	if _, ok := state["preference"]; ok {
		t.Fatalf("empty preference was fabricated: %v", state["preference"])
	}
	history, ok := state["history"].([]any)
	if !ok || len(history) != 0 {
		t.Fatalf("empty durable history = %#v", state["history"])
	}
}

func TestRevisionZeroRejectsCrossUserBootstrap(t *testing.T) {
	store := NewGORMStore(nil)
	ctx := policy.WithIdentity(context.Background(), policy.Identity{
		UserID: "user-a",
		Role:   policy.RoleViewer,
		Scope:  policy.Scope{UserID: "user-a"},
	})
	if _, err := store.BootstrapRevisionZero(ctx, "session-b", "user-b"); err == nil {
		t.Fatal("cross-user Revision 0 bootstrap reached persistence")
	}
}
