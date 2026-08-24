package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"SentinelOps/internal/dao/mysql"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	driver "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type p10RegisteredState struct {
	Marker string
}

func init() {
	schema.Register[p10RegisteredState]()
}

func TestEinoCheckpointStoreOfficialContract(t *testing.T) {
	var _ adk.CheckPointStore = (*GORMStore)(nil)
	var _ adk.CheckPointDeleter = (*GORMStore)(nil)
}

func TestEinoCheckpointGetDistinguishesMissingAndEmpty(t *testing.T) {
	db := newP07Database(t, "p10_empty")
	store, token := p09ClaimRun(t, db, "p10-empty", time.Hour)
	ctx := p10LeaseContext(t, token)
	checkpointID := p10CheckpointID(t, token.RunID)

	payload, exists, err := store.Get(context.Background(), checkpointID)
	if err != nil || exists || payload != nil {
		t.Fatalf("missing checkpoint = payload %v exists %v err %v", payload, exists, err)
	}
	if err := store.Set(ctx, checkpointID, []byte{}); err != nil {
		t.Fatalf("set empty checkpoint: %v", err)
	}
	payload, exists, err = store.Get(context.Background(), checkpointID)
	if err != nil || !exists || len(payload) != 0 {
		t.Fatalf("empty checkpoint = payload %v exists %v err %v", payload, exists, err)
	}
}

func TestEinoCheckpointSetRoundTripsOpaqueMetadataAndIsIdempotent(t *testing.T) {
	db := newP07Database(t, "p10_roundtrip")
	store, token := p09ClaimRun(t, db, "p10-roundtrip", time.Hour)
	ctx := p10LeaseContext(t, token)
	checkpointID := p10CheckpointID(t, token.RunID)
	payload := []byte{0x00, 0xff, 0x01, '{', 'n', 'o', 't', '-', 'j', 's', 'o', 'n', '}'}

	if err := store.Set(ctx, checkpointID, payload); err != nil {
		t.Fatalf("set opaque checkpoint: %v", err)
	}
	if err := store.Set(ctx, checkpointID, payload); err != nil {
		t.Fatalf("idempotent set: %v", err)
	}

	got, exists, err := store.Get(context.Background(), checkpointID)
	if err != nil || !exists || string(got) != string(payload) {
		t.Fatalf("checkpoint round trip = %x exists %v err %v", got, exists, err)
	}
	var row mysql.WorkflowCheckpoint
	if err := db.Unscoped().Where("eino_checkpoint_id = ?", checkpointID).First(&row).Error; err != nil {
		t.Fatalf("read checkpoint metadata: %v", err)
	}
	wantSHA := sha256.Sum256(payload)
	if row.RunID != token.RunID || row.EinoCheckpointID == nil || *row.EinoCheckpointID != checkpointID ||
		row.PayloadSHA256 == nil || *row.PayloadSHA256 != hex.EncodeToString(wantSHA[:]) ||
		row.RuntimeVersion == nil || *row.RuntimeVersion != "sentinelops-test-v1" ||
		row.RuntimeCompatibilityHash == nil || *row.RuntimeCompatibilityHash != strings.Repeat("a", 64) ||
		row.LeaseGeneration == nil || *row.LeaseGeneration != token.Generation || row.CommittedAt == nil {
		t.Fatalf("checkpoint metadata = %#v", row)
	}
	if row.SnapshotJSON != `{}` {
		t.Fatalf("legacy snapshot mirror = %q, want empty compatibility object", row.SnapshotJSON)
	}
	var count int64
	if err := db.Unscoped().Model(&mysql.WorkflowCheckpoint{}).Where("eino_checkpoint_id = ?", checkpointID).Count(&count).Error; err != nil {
		t.Fatalf("count idempotent checkpoint: %v", err)
	}
	if count != 1 {
		t.Fatalf("idempotent checkpoint rows = %d, want 1", count)
	}
}

func TestEinoCheckpointStaleGenerationCannotOverwrite(t *testing.T) {
	db := newP07Database(t, "p10_stale")
	store, stale := p09ClaimRun(t, db, "p10-stale", time.Hour)
	checkpointID := p10CheckpointID(t, stale.RunID)
	if err := store.Set(p10LeaseContext(t, stale), checkpointID, []byte("generation-one")); err != nil {
		t.Fatalf("set first generation: %v", err)
	}

	p09ExpireLease(t, db, stale.RunID)
	current, ok, err := store.ClaimNextRun(context.Background(), ClaimInput{Owner: "worker-p10-current", LeaseDuration: time.Hour})
	if err != nil || !ok || current == nil {
		t.Fatalf("claim current generation = %#v ok %v err %v", current, ok, err)
	}
	if err := store.Set(p10LeaseContext(t, current.Token), checkpointID, []byte("generation-current")); err != nil {
		t.Fatalf("set current generation: %v", err)
	}
	if err := store.Set(p10LeaseContext(t, stale), checkpointID, []byte("generation-stale")); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale checkpoint error = %v, want ErrLeaseLost", err)
	}
	payload, exists, err := store.Get(context.Background(), checkpointID)
	if err != nil || !exists || string(payload) != "generation-current" {
		t.Fatalf("checkpoint after stale write = %q exists %v err %v", payload, exists, err)
	}
}

func TestEinoCheckpointRejectsCrossRunTokenAndLegacyRows(t *testing.T) {
	db := newP07Database(t, "p10_isolation")
	store, token := p09ClaimRun(t, db, "p10-isolation", time.Hour)
	ctx := p10LeaseContext(t, token)
	checkpointID := p10CheckpointID(t, token.RunID)
	if err := store.Set(context.Background(), checkpointID, []byte("missing-token")); !errors.Is(err, ErrCheckpointContextMissing) {
		t.Fatalf("missing-token checkpoint error = %v, want ErrCheckpointContextMissing", err)
	}
	otherID := p10CheckpointID(t, "other-run")
	if err := store.Set(ctx, otherID, []byte("cross-run")); !errors.Is(err, ErrCheckpointIDMismatch) {
		t.Fatalf("cross-run checkpoint error = %v, want ErrCheckpointIDMismatch", err)
	}

	legacyID := "legacy-p10-checkpoint"
	if err := db.Create(&mysql.WorkflowCheckpoint{
		ID: legacyID, RunID: token.RunID, CheckpointKey: otherID, SnapshotJSON: `{"legacy":true}`,
	}).Error; err != nil {
		t.Fatalf("create legacy checkpoint: %v", err)
	}
	payload, exists, err := store.Get(context.Background(), otherID)
	if err != nil || exists || payload != nil {
		t.Fatalf("legacy row exposed as Eino checkpoint = payload %v exists %v err %v", payload, exists, err)
	}
}

func TestEinoCheckpointDeleteRequiresTerminalRun(t *testing.T) {
	db := newP07Database(t, "p10_delete")
	store, token := p09ClaimRun(t, db, "p10-delete", time.Hour)
	checkpointID := p10CheckpointID(t, token.RunID)
	if err := store.Set(p10LeaseContext(t, token), checkpointID, []byte("terminal-cleanup")); err != nil {
		t.Fatalf("set checkpoint for delete: %v", err)
	}
	if err := store.Delete(context.Background(), checkpointID); !errors.Is(err, ErrCheckpointDeleteDenied) {
		t.Fatalf("active checkpoint delete error = %v, want ErrCheckpointDeleteDenied", err)
	}

	userCtx := p08UserContext("user-p09-p10-delete")
	if err := store.CompleteRunAndCommitSession(userCtx, CompleteRunInput{
		RunID: token.RunID, ExpectedStatus: RunStatusRunning, TargetStatus: RunStatusFailed,
		Lease: token, ErrorMessage: "test terminal cleanup", TraceQuality: "complete",
	}); err != nil {
		t.Fatalf("complete run before delete: %v", err)
	}
	if err := store.Delete(context.Background(), checkpointID); err != nil {
		t.Fatalf("delete terminal checkpoint: %v", err)
	}
	if err := store.Delete(context.Background(), checkpointID); err != nil {
		t.Fatalf("idempotent terminal delete: %v", err)
	}
	if payload, exists, err := store.Get(context.Background(), checkpointID); err != nil || exists || payload != nil {
		t.Fatalf("deleted checkpoint = payload %v exists %v err %v", payload, exists, err)
	}
}

func TestCheckpointCrossProcessRegisteredState(t *testing.T) {
	db := newP07Database(t, "p10_cross_process")
	store, token := p09ClaimRun(t, db, "p10-cross-process", time.Hour)
	ctx := p10LeaseContext(t, token)
	checkpointID := p10CheckpointID(t, token.RunID)
	option, err := EinoCheckpointOption(token.RunID)
	if err != nil {
		t.Fatalf("build checkpoint option: %v", err)
	}
	want := p10RegisteredState{Marker: "registered-across-processes"}
	runner := adk.NewRunner(context.Background(), adk.RunnerConfig{
		Agent:           &p10CheckpointAgent{state: want},
		CheckPointStore: store,
	})
	p10DrainEvents(t, runner.Query(ctx, "checkpoint", option))

	childDSN := p10CurrentDatabaseDSN(t, db)
	command := exec.Command(os.Args[0], "-test.run=^TestCheckpointCrossProcessHelper$", "-test.v")
	command.Env = append(os.Environ(),
		"SENTINELOPS_P10_CHILD=1",
		"SENTINELOPS_P10_CHILD_DSN="+childDSN,
		"SENTINELOPS_P10_RUN_ID="+token.RunID,
		"SENTINELOPS_P10_OWNER="+token.Owner,
		"SENTINELOPS_P10_GENERATION="+strconv.FormatUint(token.Generation, 10),
		"SENTINELOPS_P10_CHECKPOINT_ID="+checkpointID,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("cross-process resume failed: %v\n%s", err, output)
	}
}

func TestCheckpointCrossProcessHelper(t *testing.T) {
	if os.Getenv("SENTINELOPS_P10_CHILD") != "1" {
		t.Skip("helper runs only in the child process")
	}
	generation, err := strconv.ParseUint(os.Getenv("SENTINELOPS_P10_GENERATION"), 10, 64)
	if err != nil {
		t.Fatalf("parse child generation: %v", err)
	}
	db, err := gorm.Open(gormmysql.Open(os.Getenv("SENTINELOPS_P10_CHILD_DSN")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open child checkpoint database: %v", err)
	}
	store := NewGORMStore(db)
	ctx, err := ContextWithLeaseToken(context.Background(), LeaseToken{
		RunID: os.Getenv("SENTINELOPS_P10_RUN_ID"), Owner: os.Getenv("SENTINELOPS_P10_OWNER"), Generation: generation,
	})
	if err != nil {
		t.Fatalf("build child lease context: %v", err)
	}
	runner := adk.NewRunner(context.Background(), adk.RunnerConfig{
		Agent:           &p10CheckpointAgent{state: p10RegisteredState{Marker: "registered-across-processes"}},
		CheckPointStore: store,
	})
	iterator, err := runner.Resume(ctx, os.Getenv("SENTINELOPS_P10_CHECKPOINT_ID"))
	if err != nil {
		t.Fatalf("resume child checkpoint: %v", err)
	}
	p10DrainEvents(t, iterator)
}

type p10CheckpointAgent struct {
	state p10RegisteredState
}

func (*p10CheckpointAgent) Name(context.Context) string { return "p10-checkpoint-agent" }

func (*p10CheckpointAgent) Description(context.Context) string {
	return "P10 checkpoint contract agent"
}

func (a *p10CheckpointAgent) Run(ctx context.Context, _ *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	generator.Send(adk.StatefulInterrupt(ctx, "cross-process", a.state))
	generator.Close()
	return iterator
}

func (a *p10CheckpointAgent) Resume(_ context.Context, info *adk.ResumeInfo, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	state, ok := info.InterruptState.(p10RegisteredState)
	if !ok || state != a.state {
		generator.Send(&adk.AgentEvent{Err: fmt.Errorf("registered state = %#v (%T), want %#v", info.InterruptState, info.InterruptState, a.state)})
	}
	generator.Close()
	return iterator
}

func p10DrainEvents(t *testing.T, iterator *adk.AsyncIterator[*adk.AgentEvent]) {
	t.Helper()
	for {
		event, ok := iterator.Next()
		if !ok {
			return
		}
		if event.Err != nil {
			t.Fatalf("runner event: %v", event.Err)
		}
	}
}

func p10LeaseContext(t *testing.T, token LeaseToken) context.Context {
	t.Helper()
	ctx, err := ContextWithLeaseToken(context.Background(), token)
	if err != nil {
		t.Fatalf("build lease context: %v", err)
	}
	return ctx
}

func p10CheckpointID(t *testing.T, runID string) string {
	t.Helper()
	checkpointID, err := EinoCheckpointID(runID)
	if err != nil {
		t.Fatalf("derive checkpoint id: %v", err)
	}
	return checkpointID
}

func p10CurrentDatabaseDSN(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var databaseName string
	if err := db.Raw("SELECT DATABASE()").Scan(&databaseName).Error; err != nil {
		t.Fatalf("read current test database: %v", err)
	}
	config, err := driver.ParseDSN(os.Getenv("SENTINELOPS_TEST_DSN"))
	if err != nil {
		t.Fatalf("parse base test DSN: %v", err)
	}
	config.DBName = databaseName
	return config.FormatDSN()
}
