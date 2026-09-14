package runtime

import (
	appconfig "SentinelOps/internal/config"
	"SentinelOps/internal/dao/mysql"
	"SentinelOps/utility/middleware"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/google/uuid"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	apiRuntime "SentinelOps/api/runtime"
	v1 "SentinelOps/api/runtime/v1"
	"SentinelOps/internal/ai/workflow"
	service "SentinelOps/internal/service/runtime"
)

func TestRuntimeControllerImplementsFrozenInterface(t *testing.T) {
	var controller apiRuntime.IRuntimeV1 = NewV1(nil)
	_ = controller
}

func TestRuntimeControllerHViewsAreExplicitlyUnavailable(t *testing.T) {
	controller := NewV1(nil)

	capabilities, err := controller.GetCapabilities(context.Background(), &v1.GetCapabilitiesReq{})
	if err != nil || capabilities.Availability != v1.AvailabilityUnavailable || !capabilities.NotRun || capabilities.ReasonCode != "not_observed" {
		t.Fatalf("capabilities=%+v err=%v", capabilities, err)
	}
	safety, err := controller.GetSafety(context.Background(), &v1.GetSafetyReq{})
	if err != nil || safety.Availability != v1.AvailabilityUnavailable || !safety.NotRun || safety.Item.Availability != v1.AvailabilityUnavailable {
		t.Fatalf("safety=%+v err=%v", safety, err)
	}
	worker, err := controller.GetWorkerHealth(context.Background(), &v1.GetWorkerHealthReq{})
	if err != nil || worker.Availability != v1.AvailabilityUnavailable || !worker.NotRun || len(worker.Items) != 0 {
		t.Fatalf("worker=%+v err=%v", worker, err)
	}
	eval, err := controller.GetEval(context.Background(), &v1.GetEvalReq{Page: 1, PageSize: 50, Suite: "runtime"})
	if err != nil || eval.Availability != v1.AvailabilityUnavailable || !eval.NotRun || eval.Item.Suite != "runtime" {
		t.Fatalf("eval=%+v err=%v", eval, err)
	}
	release, err := controller.GetRelease(context.Background(), &v1.GetReleaseReq{})
	if err != nil || release.Availability != v1.AvailabilityUnavailable || !release.NotRun {
		t.Fatalf("release=%+v err=%v", release, err)
	}
	retention, err := controller.GetRetention(context.Background(), &v1.GetRetentionReq{})
	if err != nil || retention.Availability != v1.AvailabilityUnavailable || !retention.NotRun {
		t.Fatalf("retention=%+v err=%v", retention, err)
	}
}

func TestRuntimeControllerH03ViewsUseServiceReadModels(t *testing.T) {
	controller := NewV1(service.NewRuntimeService(nil))

	eval, err := controller.GetEval(context.Background(), &v1.GetEvalReq{Page: 1, PageSize: 50, Suite: "runtime"})
	if err != nil {
		t.Fatal(err)
	}
	if eval.Item.Suite != service.EvalSuiteAgent || !eval.Item.NotRun || eval.Item.ReasonCode != "eval_not_executed" {
		t.Fatalf("eval=%+v", eval)
	}

	release, err := controller.GetRelease(context.Background(), &v1.GetReleaseReq{})
	if err != nil {
		t.Fatal(err)
	}
	if release.Item.RuntimeVersion == "" || release.Item.GrayState != nil || release.Item.RollbackState != nil || !release.NotRun || release.ReasonCode != "not_observed" {
		t.Fatalf("release=%+v", release)
	}
}

func TestRuntimeControllerHViewsRejectNilRequests(t *testing.T) {
	controller := NewV1(nil)
	checks := []struct {
		name string
		call func() error
	}{
		{name: "capabilities", call: func() error { _, err := controller.GetCapabilities(context.Background(), nil); return err }},
		{name: "safety", call: func() error { _, err := controller.GetSafety(context.Background(), nil); return err }},
		{name: "worker-health", call: func() error { _, err := controller.GetWorkerHealth(context.Background(), nil); return err }},
		{name: "release", call: func() error { _, err := controller.GetRelease(context.Background(), nil); return err }},
		{name: "retention", call: func() error { _, err := controller.GetRetention(context.Background(), nil); return err }},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			err := check.call()
			if err == nil || !strings.Contains(err.Error(), service.ErrorCodeRuntimeInvalidFilter) {
				t.Fatalf("error=%v, want stable 400 runtime validation", err)
			}
		})
	}
}

func TestRuntimeControllerTimeRangeIsUTCAndOrdered(t *testing.T) {
	from, to, err := parseTimeRange("2026-09-01T00:00:00+08:00", "2026-09-01T02:00:00+08:00")
	if err != nil {
		t.Fatal(err)
	}
	if !from.Equal(time.Date(2026, 8, 31, 16, 0, 0, 0, time.UTC)) || !to.Equal(time.Date(2026, 8, 31, 18, 0, 0, 0, time.UTC)) {
		t.Fatalf("from=%s to=%s", from, to)
	}
	if _, _, err := parseTimeRange("2026-09-01T02:00:00Z", "2026-09-01T01:00:00Z"); !errors.Is(err, v1.ErrRuntimeRequestValidation) {
		t.Fatalf("unordered range error=%v", err)
	}
}

func TestRuntimeControllerRejectsUnsupportedTimelineFilters(t *testing.T) {
	controller := NewV1(nil)
	for name, req := range map[string]*v1.GetTimelineReq{
		"negative attempt": {RunID: "run-1", Page: 1, PageSize: 50, Attempt: -1},
		"unsupported sort": {RunID: "run-1", Page: 1, PageSize: 50, Sort: "attempt"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := controller.GetTimeline(context.Background(), req)
			if err == nil || !strings.Contains(err.Error(), service.ErrorCodeRuntimeInvalidFilter) {
				t.Fatalf("GetTimeline() error=%v, want request validation", err)
			}
		})
	}
}

func TestRuntimeControllerUninitializedStoreReturnsStableError(t *testing.T) {
	controller := NewV1(service.NewRuntimeService(nil))
	_, err := controller.ListRuns(context.Background(), &v1.ListRunsReq{})
	if err == nil || !strings.Contains(err.Error(), service.ErrorCodeRuntimeInternal) {
		t.Fatalf("ListRuns() error=%v, want stable internal runtime error", err)
	}
}

func TestRuntimeControllerErrorDoesNotExposeUnderlyingMessage(t *testing.T) {
	controller := NewV1(nil)
	err := controller.fail(context.Background(), errors.New("password=super-secret"))
	if err == nil || strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("error=%v", err)
	}
	if !strings.Contains(err.Error(), service.ErrorCodeRuntimeInternal) {
		t.Fatalf("error=%v", err)
	}
}

func TestRuntimeControllerOperationMappingPreservesTerminalFacts(t *testing.T) {
	now := time.Now().UTC()
	op := workflow.Operation{
		OperationID: "op-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		RunID:       "run-1", Action: workflow.OperationActionReplay,
		Status: workflow.OperationStatusSucceeded, Terminal: true,
		AcceptedAt: now, FinishedAt: &now, ResultReason: "completed", CorrelationSeq: 7,
	}
	dto := mapOperation(op, false)
	if dto.Status != v1.OperationStatusSucceeded || !dto.Terminal || dto.CorrelationSeq != 7 || dto.Reason != "completed" {
		t.Fatalf("dto=%+v", dto)
	}
	if dto.Availability != v1.AvailabilityAvailable || dto.DataQuality != v1.DataQualityComplete {
		t.Fatalf("dto metadata=%+v", dto.ResourceMeta)
	}

	dto = mapOperation(op, true)
	if !dto.IdempotentReplay {
		t.Fatal("idempotent replay marker was lost")
	}
}

func TestRuntimeControllerSSEEventTypeIsHeaderSafe(t *testing.T) {
	for _, test := range []struct {
		value string
		want  string
	}{
		{value: "effect.succeeded", want: "effect.succeeded"},
		{value: "operation/failed", want: "runtime.event"},
		{value: "", want: "runtime.event"},
	} {
		if got := safeSSEEventType(test.value); got != test.want {
			t.Fatalf("safeSSEEventType(%q)=%q want %q", test.value, got, test.want)
		}
	}
}

func TestReadModelPaginationHTTPBinding(t *testing.T) {
	server := g.Server("runtime-pages-" + uuid.NewString())
	server.SetDumpRouterMap(false)
	server.SetPort(0)
	server.Group("/api", func(group *ghttp.RouterGroup) {
		group.Middleware(middleware.ResponseMiddleware)
		group.Bind(NewV1(nil))
	})
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Shutdown() })
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 5 * time.Second}
	for _, resource := range []string{"capabilities", "worker-health"} {
		for _, tc := range []struct {
			query              string
			status, page, size int
		}{
			{"", 200, 1, 50}, {"?page=2", 200, 2, 50}, {"?page_size=100", 200, 1, 100}, {"?page=3&page_size=1", 200, 3, 1},
			{"?page=0", 400, 0, 0}, {"?page=-1", 400, 0, 0}, {"?page_size=0", 400, 0, 0}, {"?page_size=-1", 400, 0, 0}, {"?page_size=101", 400, 0, 0},
			{"?page=abc", 400, 0, 0}, {"?page=1.5", 400, 0, 0}, {"?page_size=abc", 400, 0, 0}, {"?page=999999999999999999999999", 400, 0, 0},
		} {
			t.Run(resource+tc.query, func(t *testing.T) {
				res, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/runtime/v1/%s%s", server.GetListenedPort(), resource, tc.query))
				if err != nil {
					t.Fatal(err)
				}
				defer res.Body.Close()
				body, err := io.ReadAll(res.Body)
				if err != nil {
					t.Fatal(err)
				}
				if res.StatusCode != tc.status {
					t.Fatalf("status=%d want=%d body=%s", res.StatusCode, tc.status, body)
				}
				var envelope struct {
					Message string `json:"message"`
					Data    struct {
						Items []json.RawMessage `json:"items"`
						Page  v1.PageMeta       `json:"page"`
						v1.ResourceMeta
					} `json:"data"`
				}
				if err := json.Unmarshal(body, &envelope); err != nil {
					t.Fatal(err)
				}
				if tc.status == 200 && (envelope.Data.Page != (v1.PageMeta{Page: tc.page, PageSize: tc.size}) || envelope.Data.Items == nil || envelope.Data.ReasonCode != "not_observed" || !envelope.Data.NotRun) {
					t.Fatalf("body=%s", body)
				}
			})
		}
	}
}

func TestReadModelControllerForwardsPaginationToPersistedService(t *testing.T) {
	dsn := os.Getenv("SENTINELOPS_TEST_DSN")
	if dsn == "" {
		t.Skip("disposable MySQL required")
	}
	db, err := gorm.Open(gormmysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&mysql.RuntimeWorkerSnapshot{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	observed := "[]"
	rows := make([]mysql.RuntimeWorkerSnapshot, 0, 53)
	ids := make([]string, 0, 53)
	for i := 0; i < 53; i++ {
		id := fmt.Sprintf("batch3-controller-%02d", i)
		ids = append(ids, id)
		row := mysql.RuntimeWorkerSnapshot{WorkerID: id, HeartbeatAt: &now, ObservedMCPJSON: &observed, ObservedSkillJSON: &observed}
		if i > 50 {
			row.HeartbeatAt = nil
		}
		rows = append(rows, row)
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Where("worker_id IN ?", ids).Delete(&mysql.RuntimeWorkerSnapshot{}) })
	cfg := &appconfig.Config{MCP: appconfig.MCPConfig{Servers: map[string]appconfig.MCPServer{}}}
	for i := 0; i < 105; i++ {
		cfg.MCP.Servers[fmt.Sprintf("server-%03d", i)] = appconfig.MCPServer{Transport: "stdio", Command: "/bin/echo", CWD: "/tmp"}
	}
	controller := NewV1(&service.RuntimeService{Store: workflow.NewGORMStore(db), Config: cfg})
	all, err := controller.GetCapabilities(context.Background(), &v1.GetCapabilitiesReq{})
	if err != nil || len(all.Items) <= 100 {
		t.Fatalf("catalog=%+v err=%v", all, err)
	}
	workers, err := controller.GetWorkerHealth(context.Background(), &v1.GetWorkerHealthReq{})
	if err != nil || len(workers.Items) != 53 || *workers.Aggregate.Total != 51 {
		t.Fatalf("workers=%+v err=%v", workers, err)
	}
	for _, page := range []int{1, 2, 9} {
		size := 50
		request := v1.OptionalPageRequest{Page: &page, PageSize: &size}
		got, err := controller.GetCapabilities(context.Background(), &v1.GetCapabilitiesReq{OptionalPageRequest: request})
		if err != nil || got.Page.Page != page || got.Page.PageSize != size || got.Page.Total != all.Page.Total || got.ResourceMeta != all.ResourceMeta || len(got.Items) > size {
			t.Fatalf("capabilities=%+v err=%v", got, err)
		}
		health, err := controller.GetWorkerHealth(context.Background(), &v1.GetWorkerHealthReq{OptionalPageRequest: request})
		if err != nil || health.Page.Page != page || health.Page.Total != 53 || health.ResourceMeta != workers.ResourceMeta || !reflect.DeepEqual(health.Aggregate, workers.Aggregate) {
			t.Fatalf("health=%+v err=%v", health, err)
		}
		want := map[int]int{1: 50, 2: 3, 9: 0}[page]
		if len(health.Items) != want {
			t.Fatalf("page %d items=%d", page, len(health.Items))
		}
	}
	// A structurally malformed persisted observation must affect even pages
	// whose catalog items are all locally configured.
	if err := db.Model(&mysql.RuntimeWorkerSnapshot{}).Where("worker_id = ?", ids[0]).Update("observed_mcp_json", "{}").Error; err != nil {
		t.Fatal(err)
	}
	for _, page := range []int{1, 2, 999} {
		size := 1
		got, err := controller.GetCapabilities(context.Background(), &v1.GetCapabilitiesReq{OptionalPageRequest: v1.OptionalPageRequest{Page: &page, PageSize: &size}})
		if err != nil || got.Availability != v1.AvailabilityUnavailable || got.DataQuality != v1.DataQualityUnknown || got.ReasonCode != "worker_snapshot_malformed" || !got.NotRun || got.Page.Total != all.Page.Total {
			t.Fatalf("malformed catalog=%+v err=%v", got, err)
		}
	}

}
