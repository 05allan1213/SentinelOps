package runtime

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	driver "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestRecoverySelectorChoosesResumeReplayOrParked(t *testing.T) {
	const compatible = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	tests := []struct {
		name       string
		facts      workflow.RecoveryFacts
		current    string
		wantMode   workflow.RecoveryMode
		wantReason string
	}{
		{
			name: "complete compatible checkpoint resumes",
			facts: workflow.RecoveryFacts{
				RunID: "run-resume", RuntimeCompatibilityHash: compatible,
				Checkpoint: workflow.RecoveryCheckpoint{State: workflow.RecoveryCheckpointValid, ID: "sentinelops/run-resume"},
			},
			current: compatible, wantMode: workflow.RecoveryModeResume,
		},
		{
			name: "never checkpointed without dependency replays immutable query",
			facts: workflow.RecoveryFacts{
				RunID: "run-replay", RuntimeCompatibilityHash: compatible, ImmutableQuery: "durable query",
				Checkpoint: workflow.RecoveryCheckpoint{State: workflow.RecoveryCheckpointMissing},
			},
			current: compatible, wantMode: workflow.RecoveryModeReplay,
		},
		{
			name: "published approval without checkpoint parks",
			facts: workflow.RecoveryFacts{
				RunID: "run-missing", RuntimeCompatibilityHash: compatible, HasPublishedApproval: true,
				Checkpoint: workflow.RecoveryCheckpoint{State: workflow.RecoveryCheckpointMissing},
			},
			current: compatible, wantMode: workflow.RecoveryModeParked, wantReason: workflow.ParkReasonCheckpointMissing,
		},
		{
			name: "corrupt checkpoint parks even without another dependency",
			facts: workflow.RecoveryFacts{
				RunID: "run-corrupt", RuntimeCompatibilityHash: compatible,
				Checkpoint: workflow.RecoveryCheckpoint{State: workflow.RecoveryCheckpointCorrupt, ID: "sentinelops/run-corrupt"},
			},
			current: compatible, wantMode: workflow.RecoveryModeParked, wantReason: workflow.ParkReasonCheckpointCorrupt,
		},
		{
			name: "runtime mismatch always parks",
			facts: workflow.RecoveryFacts{
				RunID: "run-incompatible", RuntimeCompatibilityHash: compatible, ImmutableQuery: "durable query",
				Checkpoint: workflow.RecoveryCheckpoint{State: workflow.RecoveryCheckpointMissing},
			},
			current: strings.Repeat("b", 64), wantMode: workflow.RecoveryModeParked, wantReason: workflow.ParkReasonRuntimeIncompatible,
		},
		{
			name: "effect unknown cannot be unlocked by recovery selector",
			facts: workflow.RecoveryFacts{
				RunID: "run-effect", Status: workflow.RunStatusParked, ParkReason: workflow.ParkReasonEffectUnknown,
				RuntimeCompatibilityHash: compatible,
				Checkpoint:               workflow.RecoveryCheckpoint{State: workflow.RecoveryCheckpointValid, ID: "sentinelops/run-effect"},
			},
			current: compatible, wantMode: workflow.RecoveryModeParked, wantReason: workflow.ParkReasonEffectUnknown,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision, err := SelectRecovery(test.facts, test.current)
			if err != nil {
				t.Fatalf("select recovery: %v", err)
			}
			if decision.Mode != test.wantMode || decision.ParkReason != test.wantReason {
				t.Fatalf("decision = %+v, want mode=%s reason=%s", decision, test.wantMode, test.wantReason)
			}
			if decision.Mode == workflow.RecoveryModeReplay && decision.ImmutableQuery != test.facts.ImmutableQuery {
				t.Fatalf("replay query = %q, want immutable %q", decision.ImmutableQuery, test.facts.ImmutableQuery)
			}
		})
	}
}

func TestResumeAndReplayUseOfficialRunnerAndSafeSessionIdentity(t *testing.T) {
	store := &p12MemoryCheckpointStore{values: map[string][]byte{}}
	agent := &p12RecoveryAgent{state: p12RegisteredRecoveryState{Marker: "opaque-state"}}
	runner, err := NewDurableRunner(context.Background(), agent, store, false)
	if err != nil {
		t.Fatalf("new durable runner: %v", err)
	}
	attempt := p12Attempt("run-p12", 2, "trace-replay")
	replay := RecoveryDecision{Mode: workflow.RecoveryModeReplay, ImmutableQuery: "immutable replay query"}
	execution, err := InvokeRecoveryRunner(context.Background(), runner, attempt, replay, nil)
	if err != nil {
		t.Fatalf("invoke replay: %v", err)
	}
	p12DrainRuntimeEvents(t, execution.Events)
	if agent.runQuery != "immutable replay query" || agent.runID != attempt.Run.ID {
		t.Fatalf("replay observed query/run = %q/%q", agent.runQuery, agent.runID)
	}

	attempt2 := p12Attempt("run-p12", 3, "trace-resume")
	checkpointID, err := workflow.EinoCheckpointID(attempt2.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	resume := RecoveryDecision{Mode: workflow.RecoveryModeResume, CheckpointID: checkpointID}
	execution, err = InvokeRecoveryRunner(context.Background(), runner, attempt2, resume, &adk.ResumeParams{Targets: map[string]any{}})
	if err != nil {
		t.Fatalf("invoke resume: %v", err)
	}
	p12DrainRuntimeEvents(t, execution.Events)
	if !agent.resumed || agent.runID != attempt2.Run.ID {
		t.Fatalf("resume state = resumed %v run %q", agent.resumed, agent.runID)
	}
	if attempt.Run.ID != attempt2.Run.ID || attempt.Run.Attempt == attempt2.Run.Attempt || attempt.Trace.ID == attempt2.Trace.ID {
		t.Fatalf("attempt identity was not rotated while run stayed stable: before=%+v after=%+v", attempt, attempt2)
	}
	budget := &p12BudgetHandle{}
	attempt.Budget, attempt2.Budget = budget, budget
	attempt.History, attempt2.History = []byte(`{"durable":true}`), []byte(`{"durable":true}`)
	if attempt.Budget != attempt2.Budget || string(attempt.History) != string(attempt2.History) ||
		attempt.Run.RuntimeCompatibilityHash != attempt2.Run.RuntimeCompatibilityHash {
		t.Fatal("Resume / Replay reset durable Budget, Context or Runtime Snapshot identity")
	}
}

func TestParkedOnOpaqueCheckpointDecodeFailure(t *testing.T) {
	db := p12NewRuntimeDatabase(t, "decode_failure")
	store := workflow.NewGORMStore(db)
	frozen, err := FreezeRuntimeSnapshot(p11SnapshotInput())
	if err != nil {
		t.Fatal(err)
	}
	userID := "user-p12-decode"
	userContext := policy.WithIdentity(context.Background(), policy.Identity{
		UserID: userID, Role: policy.RoleViewer, Scope: policy.Scope{UserID: userID},
	})
	_, err = store.CreateRunWithSessionLock(userContext, workflow.CreateRunInput{
		ID: "run-p12-decode", WorkflowKey: "p12-test", SessionID: "session-p12-decode",
		QueryText: "immutable decode query", ImmutableInputJSON: json.RawMessage(`{"query":"immutable decode query"}`),
		RuntimeSnapshot: frozen.WorkflowFields(), BudgetLimitsJSON: json.RawMessage(`{}`),
		DeadlineAt: time.Now().Add(time.Hour), CreatedEvent: workflow.WorkflowEventInput{Type: workflow.EventRunCreated},
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimNextRun(context.Background(), workflow.ClaimInput{Owner: "worker-p12-decode", LeaseDuration: time.Hour})
	if err != nil || !ok {
		t.Fatalf("claim decode Run: ok=%v err=%v", ok, err)
	}
	attemptContext, attempt, err := BuildAttemptContext(context.Background(), *claimed, &p11BudgetFactory{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(attempt.Cancel)
	checkpointID, err := workflow.EinoCheckpointID(claimed.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(attemptContext, checkpointID, []byte("valid-metadata-invalid-eino-gob")); err != nil {
		t.Fatal(err)
	}
	runner, err := NewDurableRunner(context.Background(), &p12RecoveryAgent{state: p12RegisteredRecoveryState{Marker: "decode"}}, store, false)
	if err != nil {
		t.Fatal(err)
	}
	result, err := StartRecovery(attemptContext, store, runner, attempt, frozen.CompatibilityHash(), nil)
	if err == nil || result == nil || result.Decision.Mode != workflow.RecoveryModeParked ||
		result.Decision.ParkReason != workflow.ParkReasonCheckpointCorrupt {
		t.Fatalf("decode failure result = %+v err=%v", result, err)
	}
	var stored mysql.WorkflowRun
	if err := db.First(&stored, "id = ?", claimed.Run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != workflow.RunStatusParked || stored.ParkReason == nil || *stored.ParkReason != workflow.ParkReasonCheckpointCorrupt {
		t.Fatalf("decode failure did not park Run: %+v", stored)
	}
	var events []mysql.WorkflowEvent
	if err := db.Where("run_id = ?", claimed.Run.ID).Order("seq ASC").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 || events[2].EventType != workflow.EventRunResumed || events[3].EventType != workflow.EventRunParked {
		t.Fatalf("decode failure events = %+v", events)
	}
}

func TestRecursiveCancelUsesSafePointsAndImmediateOnLostLease(t *testing.T) {
	for _, boundary := range []DrainBoundary{DrainAfterChatModel, DrainAfterToolCalls} {
		t.Run(string(boundary), func(t *testing.T) {
			cancelOption, cancel := adk.WithCancel()
			agent := &p12BlockingInterruptAgent{release: make(chan struct{})}
			runner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: agent, CheckPointStore: &p12MemoryCheckpointStore{values: map[string][]byte{}}})
			iterator := runner.Query(context.Background(), "cancel", cancelOption, adk.WithCheckPointID("cancel-"+string(boundary)))
			handle, err := RequestDrain(cancel, boundary, time.Second)
			if err != nil {
				t.Fatalf("request drain: %v", err)
			}
			close(agent.release)
			var cancelError *adk.CancelError
			for {
				event, ok := iterator.Next()
				if !ok {
					break
				}
				if errors.As(event.Err, &cancelError) {
					break
				}
			}
			if err := handle.Wait(); err != nil {
				t.Fatalf("wait drain: %v", err)
			}
			wantMode := adk.CancelAfterChatModel
			if boundary == DrainAfterToolCalls {
				wantMode = adk.CancelAfterToolCalls
			}
			if cancelError == nil || cancelError.Info == nil || cancelError.Info.Mode != wantMode {
				t.Fatalf("cancel error = %+v, want mode %v", cancelError, wantMode)
			}
		})
	}

	cancelOption, cancel := adk.WithCancel()
	agent := &p12BlockingInterruptAgent{release: make(chan struct{})}
	runner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: agent, CheckPointStore: &p12MemoryCheckpointStore{values: map[string][]byte{}}})
	iterator := runner.Query(context.Background(), "lost lease", cancelOption, adk.WithCheckPointID("cancel-immediate"))
	handle, err := RequestLostLeaseCancel(cancel)
	if err != nil {
		t.Fatalf("request lost-lease cancel: %v", err)
	}
	close(agent.release)
	p12DrainAllowCancel(t, iterator)
	if err := handle.Wait(); err != nil {
		t.Fatalf("wait immediate cancel: %v", err)
	}
}

func TestRecursiveCancelPropagatesThroughAgentTool(t *testing.T) {
	childStarted := make(chan struct{}, 1)
	childRelease := make(chan struct{})
	childModel := &p12AgentToolModel{kind: "child", started: childStarted, release: childRelease}
	child, err := adk.NewChatModelAgent(context.Background(), &adk.ChatModelAgentConfig{
		Name: "p12-child", Description: "P12 nested child", Model: childModel, GenModelInput: LiteralGenModelInput,
	})
	if err != nil {
		t.Fatal(err)
	}
	inner, err := adk.NewSequentialAgent(context.Background(), &adk.SequentialAgentConfig{
		Name: "p12-inner", Description: "P12 nested workflow", SubAgents: []adk.Agent{child},
	})
	if err != nil {
		t.Fatal(err)
	}
	agentTool := adk.NewAgentTool(context.Background(), inner)
	rootModel := &p12AgentToolModel{kind: "root"}
	root, err := adk.NewChatModelAgent(context.Background(), &adk.ChatModelAgentConfig{
		Name: "p12-root", Description: "P12 nested root", Model: rootModel, GenModelInput: LiteralGenModelInput,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: []tool.BaseTool{agentTool}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	store := &p12MemoryCheckpointStore{values: map[string][]byte{}}
	runner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: root, CheckPointStore: store})
	cancelOption, cancel := adk.WithCancel()
	iterator := runner.Query(context.Background(), "nested", cancelOption, adk.WithCheckPointID("p12-nested"))
	select {
	case <-childStarted:
	case <-time.After(time.Second):
		t.Fatal("nested AgentTool child model did not start")
	}
	handle, err := RequestLostLeaseCancel(cancel)
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Wait(); err != nil {
		t.Fatalf("recursive immediate cancel did not complete: %v", err)
	}
	p12DrainAllowCancel(t, iterator)
	if _, ok := store.values["p12-nested"]; !ok {
		t.Fatal("recursive AgentTool cancel did not persist root checkpoint")
	}

	resumeOrder := make(chan string, 2)
	resumeChild, err := adk.NewChatModelAgent(context.Background(), &adk.ChatModelAgentConfig{
		Name: "p12-child", Description: "P12 nested child", Model: &p12ResumeOrderModel{label: "child", calls: resumeOrder}, GenModelInput: LiteralGenModelInput,
	})
	if err != nil {
		t.Fatal(err)
	}
	resumeInner, err := adk.NewSequentialAgent(context.Background(), &adk.SequentialAgentConfig{
		Name: "p12-inner", Description: "P12 nested workflow", SubAgents: []adk.Agent{resumeChild},
	})
	if err != nil {
		t.Fatal(err)
	}
	resumeRoot, err := adk.NewChatModelAgent(context.Background(), &adk.ChatModelAgentConfig{
		Name: "p12-root", Description: "P12 nested root", Model: &p12ResumeOrderModel{label: "root", calls: resumeOrder}, GenModelInput: LiteralGenModelInput,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: []tool.BaseTool{adk.NewAgentTool(context.Background(), resumeInner)}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	resumeRunner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: resumeRoot, CheckPointStore: store})
	resumeIterator, err := resumeRunner.Resume(context.Background(), "p12-nested")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case first := <-resumeOrder:
		if first != "child" {
			t.Fatalf("first resumed model = %q, want nested AgentTool child", first)
		}
	case <-time.After(time.Second):
		t.Fatal("recursive AgentTool checkpoint did not resume")
	}
	p12DrainRuntimeEvents(t, resumeIterator)
}

type p12RegisteredRecoveryState struct{ Marker string }

func init() { schema.Register[p12RegisteredRecoveryState]() }

type p12RecoveryAgent struct {
	state    p12RegisteredRecoveryState
	runQuery string
	runID    string
	resumed  bool
}

func (*p12RecoveryAgent) Name(context.Context) string        { return "p12-recovery-agent" }
func (*p12RecoveryAgent) Description(context.Context) string { return "P12 recovery contract agent" }

func (a *p12RecoveryAgent) Run(ctx context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	if len(input.Messages) > 0 {
		a.runQuery = input.Messages[len(input.Messages)-1].Content
	}
	if value, ok := adk.GetSessionValue(ctx, SessionRunIDKey); ok {
		a.runID, _ = value.(string)
	}
	generator.Send(adk.StatefulInterrupt(ctx, "p12-recovery", a.state))
	generator.Close()
	return iterator
}

func (a *p12RecoveryAgent) Resume(ctx context.Context, info *adk.ResumeInfo, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	state, ok := info.InterruptState.(p12RegisteredRecoveryState)
	if !ok || state != a.state {
		generator.Send(&adk.AgentEvent{Err: errors.New("resume lost registered opaque state")})
	} else {
		a.resumed = true
	}
	if value, ok := adk.GetSessionValue(ctx, SessionRunIDKey); ok {
		a.runID, _ = value.(string)
	}
	generator.Close()
	return iterator
}

type p12BlockingInterruptAgent struct{ release chan struct{} }

func (*p12BlockingInterruptAgent) Name(context.Context) string { return "p12-blocking-agent" }
func (*p12BlockingInterruptAgent) Description(context.Context) string {
	return "P12 cancel contract agent"
}
func (a *p12BlockingInterruptAgent) Run(ctx context.Context, _ *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		<-a.release
		generator.Send(adk.StatefulInterrupt(ctx, "cancel-boundary", p12RegisteredRecoveryState{Marker: "cancel"}))
		generator.Close()
	}()
	return iterator
}

type p12MemoryCheckpointStore struct{ values map[string][]byte }

func (s *p12MemoryCheckpointStore) Get(_ context.Context, id string) ([]byte, bool, error) {
	value, ok := s.values[id]
	return append([]byte(nil), value...), ok, nil
}
func (s *p12MemoryCheckpointStore) Set(_ context.Context, id string, value []byte) error {
	s.values[id] = append([]byte(nil), value...)
	return nil
}

func p12Attempt(runID string, attempt uint, traceID string) *AttemptContext {
	return &AttemptContext{
		Run:   RunIdentity{ID: runID, Attempt: attempt, LeaseGeneration: uint64(attempt), RuntimeVersion: "runtime-v1", RuntimeCompatibilityHash: strings.Repeat("a", 64)},
		Lease: workflow.LeaseToken{RunID: runID, Owner: "worker-p12", Generation: uint64(attempt)},
		Trace: TraceIdentity{ID: traceID},
	}
}

type p12BudgetHandle struct{}

func (*p12BudgetHandle) RuntimeBudgetHandle() {}

type p12AgentToolModel struct {
	kind    string
	started chan struct{}
	release chan struct{}
	delay   time.Duration
	calls   int32
}

func (m *p12AgentToolModel) Generate(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	call := atomic.AddInt32(&m.calls, 1)
	if m.kind == "root" {
		return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{
			ID: fmt.Sprintf("p12-agent-tool-%d", call), Type: "function",
			Function: schema.FunctionCall{Name: "p12-inner", Arguments: `{"request":"nested"}`},
		}}}, nil
	}
	select {
	case m.started <- struct{}{}:
	default:
	}
	var completed <-chan time.Time
	if m.delay > 0 {
		completed = time.After(m.delay)
	}
	select {
	case <-completed:
		return schema.AssistantMessage("child complete", nil), nil
	case <-m.release:
		return schema.AssistantMessage("child complete", nil), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *p12AgentToolModel) Stream(ctx context.Context, input []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, input, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func (*p12AgentToolModel) BindTools([]*schema.ToolInfo) error { return nil }

type p12ResumeOrderModel struct {
	label string
	calls chan<- string
}

func (m *p12ResumeOrderModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	m.calls <- m.label
	return schema.AssistantMessage(m.label+" resumed", nil), nil
}

func (m *p12ResumeOrderModel) Stream(ctx context.Context, input []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, input, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func (*p12ResumeOrderModel) BindTools([]*schema.ToolInfo) error { return nil }

func p12DrainRuntimeEvents(t *testing.T, iterator *adk.AsyncIterator[*adk.AgentEvent]) {
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

func p12DrainAllowCancel(t *testing.T, iterator *adk.AsyncIterator[*adk.AgentEvent]) {
	t.Helper()
	for {
		event, ok := iterator.Next()
		if !ok {
			return
		}
		if event.Err == nil {
			continue
		}
		var cancelError *adk.CancelError
		if !errors.As(event.Err, &cancelError) {
			t.Fatalf("runner event: %v", event.Err)
		}
	}
}

func p12NewRuntimeDatabase(t *testing.T, suffix string) *gorm.DB {
	t.Helper()
	baseDSN := os.Getenv("SENTINELOPS_TEST_DSN")
	if baseDSN == "" {
		t.Fatal("SENTINELOPS_TEST_DSN is required for P12 runtime integration")
	}
	config, err := driver.ParseDSN(baseDSN)
	if err != nil {
		t.Fatalf("parse P12 test DSN: %v", err)
	}
	if config.DBName != "sentinelops_p03" {
		t.Fatalf("refuse non-disposable database %q", config.DBName)
	}
	databaseName := "sentinelops_p03_p12_runtime_" + suffix
	adminConfig := *config
	adminConfig.DBName = "mysql"
	adminDB, err := sql.Open("mysql", adminConfig.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })
	quotedName := "`" + databaseName + "`"
	if _, err := adminDB.Exec("DROP DATABASE IF EXISTS " + quotedName); err != nil {
		t.Fatal(err)
	}
	if _, err := adminDB.Exec("CREATE DATABASE " + quotedName + " CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = adminDB.Exec("DROP DATABASE IF EXISTS " + quotedName) })

	testConfig := *config
	testConfig.DBName = databaseName
	testDSN := testConfig.FormatDSN()
	gooseBinary := os.Getenv("SENTINELOPS_GOOSE_BIN")
	if gooseBinary == "" {
		gooseBinary, err = exec.LookPath("goose")
		if err != nil {
			t.Fatal("goose v3.27.3 is required for P12 runtime integration")
		}
	}
	versionOutput, err := exec.Command(gooseBinary, "-version").CombinedOutput()
	if err != nil || !strings.Contains(string(versionOutput), "v3.27.3") {
		t.Fatalf("goose version = %q err=%v, want v3.27.3", strings.TrimSpace(string(versionOutput)), err)
	}
	migrationDirectory, err := filepath.Abs(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(gooseBinary, "-dir", migrationDirectory, "mysql", testDSN, "up").CombinedOutput()
	if err != nil {
		t.Fatalf("P12 goose up: %v\n%s", err, strings.ReplaceAll(string(output), testDSN, "<redacted-dsn>"))
	}
	sqlDB, err := sql.Open("mysql", testDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	database, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return database
}
