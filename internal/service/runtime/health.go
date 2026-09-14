package runtime

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	v1 "SentinelOps/api/runtime/v1"
	"SentinelOps/internal/ai/policy"
	airuntime "SentinelOps/internal/ai/runtime"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
	settingssvc "SentinelOps/internal/service/settings"
)

// GetWorkerHealth projects only persisted runtime_worker_snapshots rows.
func (s *RuntimeService) GetWorkerHealth(ctx context.Context, pagination ...v1.PageRequest) (v1.WorkerHealthRes, error) {
	if _, _, _, err := readModelPage(0, pagination); err != nil {
		return v1.WorkerHealthRes{}, err
	}
	if s == nil || s.Store == nil || s.Store.DB() == nil {
		meta := unavailableObservedMeta()
		_, _, page, err := readModelPage(0, pagination)
		return v1.WorkerHealthRes{Items: []v1.WorkerObservationDTO{}, Page: page, Aggregate: &v1.WorkerAggregateDTO{ResourceMeta: meta}, ResourceMeta: meta}, err
	}
	queryStore := mysql.NewGORMStore(s.Store.DB())
	rows, err := queryStore.ListRuntimeWorkerSnapshots(ctx)
	if err != nil {
		return v1.WorkerHealthRes{}, err
	}
	return projectWorkerHealth(rows, s.workerLeaseDuration(), time.Now().UTC(), pagination)
}

func (s *RuntimeService) workerLeaseDuration() time.Duration {
	if s != nil && s.Config != nil && s.Config.Observability.Retention.LeaseDurationMS > 0 {
		return time.Duration(s.Config.Observability.Retention.LeaseDurationMS) * time.Millisecond
	}
	return 30 * time.Second
}

func projectWorkerHealth(rows []mysql.RuntimeWorkerSnapshot, lease time.Duration, now time.Time, pagination []v1.PageRequest) (v1.WorkerHealthRes, error) {
	if len(rows) == 0 {
		meta := unavailableObservedMeta()
		_, _, page, err := readModelPage(0, pagination)
		return v1.WorkerHealthRes{Items: []v1.WorkerObservationDTO{}, Page: page, Aggregate: &v1.WorkerAggregateDTO{ResourceMeta: meta}, ResourceMeta: meta}, err
	}
	// WorkerID is the persisted primary key; heartbeat is a deterministic
	// tie-breaker for read-model inputs before slicing.
	rows = append([]mysql.RuntimeWorkerSnapshot(nil), rows...)
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].WorkerID != rows[j].WorkerID {
			return rows[i].WorkerID < rows[j].WorkerID
		}
		if rows[i].HeartbeatAt == nil {
			return rows[j].HeartbeatAt != nil
		}
		return rows[j].HeartbeatAt != nil && rows[i].HeartbeatAt.Before(*rows[j].HeartbeatAt)
	})
	items := make([]v1.WorkerObservationDTO, 0, len(rows))
	meta := v1.ResourceMeta{Availability: v1.AvailabilityAvailable, DataQuality: v1.DataQualityComplete}
	var total, active, idle, stale int
	for _, row := range rows {
		item, rowMeta := workerObservationDTO(row, now, lease)
		items = append(items, item)
		if rowMeta.DataQuality == v1.DataQualityPartial || rowMeta.Availability == v1.AvailabilityUnavailable {
			meta.Availability = v1.AvailabilityPartial
			meta.DataQuality = v1.DataQualityPartial
			if meta.ReasonCode == "" {
				meta.ReasonCode = "malformed_worker_snapshot"
			}
		}
		if rowMeta.Availability == v1.AvailabilityUnavailable {
			continue
		}
		total++
		switch item.Status {
		case airuntime.WorkerStatusRunning, airuntime.WorkerStatusDraining:
			active++
		case airuntime.WorkerStatusIdle:
			idle++
		case airuntime.WorkerStatusStale:
			stale++
		}
	}
	if total == 0 {
		meta = unavailableObservedMeta()
	}
	ptr := func(v int) *int { return &v }
	aggMeta := v1.ResourceMeta{Availability: meta.Availability, DataQuality: meta.DataQuality, ReasonCode: meta.ReasonCode}
	var aggregate *v1.WorkerAggregateDTO
	if total > 0 {
		aggregate = &v1.WorkerAggregateDTO{Total: ptr(total), Active: ptr(active), Idle: ptr(idle), Stale: ptr(stale), ResourceMeta: aggMeta}
	} else {
		aggregate = &v1.WorkerAggregateDTO{ResourceMeta: unavailableObservedMeta()}
	}
	start, end, page, err := readModelPage(len(items), pagination)
	if err != nil {
		return v1.WorkerHealthRes{}, err
	}
	return v1.WorkerHealthRes{Items: append([]v1.WorkerObservationDTO{}, items[start:end]...), Page: page, Aggregate: aggregate, ResourceMeta: meta}, nil
}

func workerObservationDTO(row mysql.RuntimeWorkerSnapshot, now time.Time, lease time.Duration) (v1.WorkerObservationDTO, v1.ResourceMeta) {
	meta := v1.ResourceMeta{Availability: v1.AvailabilityAvailable, DataQuality: v1.DataQualityComplete}
	if strings.TrimSpace(row.WorkerID) == "" {
		meta.Availability, meta.DataQuality, meta.ReasonCode = v1.AvailabilityUnavailable, v1.DataQualityUnknown, "malformed_worker_snapshot"
	}
	status := airuntime.ClassifyObservedWorker(now, row.HeartbeatAt, lease)
	if strings.TrimSpace(row.WorkerID) == "" {
		status = airuntime.WorkerStatusUnavailable
	}
	if status == airuntime.WorkerStatusUnavailable {
		meta.Availability = v1.AvailabilityUnavailable
		meta.DataQuality = v1.DataQualityUnknown
		if meta.ReasonCode == "" {
			meta.ReasonCode = "not_observed"
		}
	}
	if status != airuntime.WorkerStatusStale && status != airuntime.WorkerStatusUnavailable && row.Status != nil {
		switch strings.TrimSpace(*row.Status) {
		case airuntime.WorkerStatusIdle, airuntime.WorkerStatusRunning, airuntime.WorkerStatusDraining:
			status = strings.TrimSpace(*row.Status)
		default:
			status = airuntime.WorkerStatusUnavailable
			meta.Availability, meta.DataQuality, meta.ReasonCode = v1.AvailabilityPartial, v1.DataQualityPartial, "malformed_worker_snapshot"
		}
	}
	mcpCount, skillCount := observedCount(row.ObservedMCPJSON), observedCount(row.ObservedSkillJSON)
	if row.ObservedMCPJSON == nil || row.ObservedSkillJSON == nil {
		meta.Availability = v1.AvailabilityUnavailable
		meta.DataQuality = v1.DataQualityUnknown
		meta.ReasonCode = "not_observed"
	}
	if (row.ObservedMCPJSON != nil && mcpCount < 0) || (row.ObservedSkillJSON != nil && skillCount < 0) {
		meta.Availability, meta.DataQuality, meta.ReasonCode = v1.AvailabilityPartial, v1.DataQualityPartial, "malformed_worker_snapshot"
		if mcpCount < 0 {
			mcpCount = 0
		}
		if skillCount < 0 {
			skillCount = 0
		}
	}
	item := v1.WorkerObservationDTO{WorkerID: row.WorkerID, Status: status, HeartbeatAt: row.HeartbeatAt, RuntimeVersion: value(row.RuntimeVersion), RuntimeCompatibilityHash: value(row.RuntimeCompatibilityHash), ActiveRunID: value(row.ActiveRunID), ActiveGeneration: valueU64(row.ActiveGeneration), ObservedMCPCount: mcpCount, ObservedSkillCount: skillCount, LastError: policyRedact(value(row.LastErrorRedacted)), ResourceMeta: meta}
	return item, meta
}

func observedCount(raw *string) int {
	if raw == nil {
		return 0
	}
	var values []airuntime.ObservedRuntimeComponent
	if json.Unmarshal([]byte(*raw), &values) != nil {
		return -1
	}
	return len(values)
}

// GetRetention returns validated settings and durable active-session protection facts.
func (s *RuntimeService) GetRetention(ctx context.Context) (v1.RetentionRes, error) {
	settings, err := settingssvc.GetRetention(ctx)
	if err != nil {
		return v1.RetentionRes{}, err
	}
	policy := airuntime.RetentionPolicy{PayloadDays: settings.PayloadDays, AuditDays: settings.AuditDays}
	item := v1.RetentionDTO{PayloadDays: settings.PayloadDays, AuditDays: settings.AuditDays, PolicyValid: policy.Validate() == nil}
	meta := v1.ResourceMeta{Availability: v1.AvailabilityAvailable, DataQuality: v1.DataQualityComplete}
	if err := policy.Validate(); err != nil {
		meta.Availability, meta.DataQuality, meta.ReasonCode = v1.AvailabilityPartial, v1.DataQualityPartial, "invalid_retention_policy"
	}
	item.ResourceMeta = meta
	if s == nil || s.Store == nil || s.Store.DB() == nil {
		item.ResourceMeta = unavailableObservedMeta()
		return v1.RetentionRes{Item: item, ResourceMeta: item.ResourceMeta}, nil
	}
	statuses, err := mysql.NewGORMStore(s.Store.DB()).ListRuntimeRunStatuses(ctx)
	if err != nil {
		return v1.RetentionRes{}, err
	}
	count := 0
	for _, status := range statuses {
		if workflow.RunOccupiesSession(status) {
			count++
		}
	}
	item.ProtectedActiveCount = count
	// Cleanup history has no durable query source in the current schema.
	item.ResourceMeta.Availability = v1.AvailabilityUnavailable
	item.ResourceMeta.DataQuality = v1.DataQualityUnknown
	item.ResourceMeta.ReasonCode = "not_observed"
	return v1.RetentionRes{Item: item, ResourceMeta: item.ResourceMeta}, nil
}

func policyRedact(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return policy.NewRedactor().RedactText(value)
}
