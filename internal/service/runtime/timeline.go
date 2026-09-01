package runtime

import (
	"context"
	"strings"
	"time"

	v1 "SentinelOps/api/runtime/v1"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
)

type TimelineFilter struct {
	EventTypes      []string
	Attempt         int
	Generation      uint64
	From, To        *time.Time
	Page, PageSize  int
	Sort, Direction string
}

func (s *RuntimeService) ListTimeline(ctx context.Context, runID string, f TimelineFilter) (v1.TimelineRes, error) {
	run, err := s.Store.GetRuntimeRun(ctx, runID)
	if err != nil {
		return v1.TimelineRes{}, MapRecoveryError(err)
	}
	if err = AuthorizeRun(ctx, run.UserID); err != nil {
		return v1.TimelineRes{}, err
	}
	q := s.Store.DB().WithContext(ctx).Where("run_id = ?", runID)
	if len(f.EventTypes) > 0 {
		q = q.Where("event_type IN ?", f.EventTypes)
	}
	if f.From != nil {
		q = q.Where("created_at >= ?", f.From.UTC())
	}
	if f.To != nil {
		q = q.Where("created_at < ?", f.To.UTC())
	}
	if f.Sort == "created_at" {
		dir := "DESC"
		if strings.EqualFold(f.Direction, "asc") {
			dir = "ASC"
		}
		q = q.Order("created_at " + dir).Order("seq " + dir)
	} else {
		dir := "DESC"
		if strings.EqualFold(f.Direction, "asc") {
			dir = "ASC"
		}
		q = q.Order("seq " + dir)
	}
	var total int64
	if err = q.Model(&mysql.WorkflowEvent{}).Count(&total).Error; err != nil {
		return v1.TimelineRes{}, err
	}
	page, size := f.Page, f.PageSize
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = 50
	}
	if size > 100 {
		size = 100
	}
	var rows []mysql.WorkflowEvent
	if err = q.Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return v1.TimelineRes{}, err
	}
	items := make([]v1.RuntimeEventDTO, 0, len(rows))
	for _, row := range rows {
		dto := MapRuntimeEvent(row)
		if f.Attempt > 0 && dto.Attempt != f.Attempt {
			continue
		}
		if f.Generation > 0 && dto.Generation != f.Generation {
			continue
		}
		items = append(items, dto)
	}
	// Correlation filters are applied after canonical mapping so malformed and
	// unknown events remain visible when no filter is requested.
	if f.Attempt > 0 || f.Generation > 0 {
		total = int64(len(items))
	}
	return v1.TimelineRes{Items: items, Page: v1.PageMeta{Page: page, PageSize: size, Total: total, HasNext: int64(page*size) < total}, ResourceMeta: v1.ResourceMeta{Availability: v1.AvailabilityAvailable, DataQuality: v1.DataQualityComplete}}, nil
}

func (s *RuntimeService) ListAttempts(ctx context.Context, runID string, p v1.PageRequest) (v1.AttemptsRes, error) {
	run, err := s.Store.GetRuntimeRun(ctx, runID)
	if err != nil {
		return v1.AttemptsRes{}, MapRecoveryError(err)
	}
	if err = AuthorizeRun(ctx, run.UserID); err != nil {
		return v1.AttemptsRes{}, err
	}
	if p.Page < 1 {
		p.Page = 1
	}
	if p.PageSize <= 0 {
		p.PageSize = 50
	}
	if p.PageSize > 100 {
		p.PageSize = 100
	}
	var rows []mysql.WorkflowAttempt
	q := s.Store.DB().WithContext(ctx).Where("run_id = ?", runID).Order("attempt ASC")
	var total int64
	q.Model(&mysql.WorkflowAttempt{}).Count(&total)
	q.Offset((p.Page - 1) * p.PageSize).Limit(p.PageSize).Find(&rows)
	quality := "complete"
	if len(rows) == 0 {
		rows, quality, err = s.Store.RebuildAttemptsFromEvents(ctx, runID)
		if err != nil {
			return v1.AttemptsRes{}, err
		}
	}
	items := make([]v1.AttemptDTO, 0, len(rows))
	for _, a := range rows {
		status := v1.RuntimeStatus(value(a.Status))
		items = append(items, v1.AttemptDTO{AttemptID: a.ID, RunID: a.RunID, Attempt: int(a.Attempt), Mode: value(a.Mode), Status: status, CurrentPhase: v1.CurrentPhase(value(a.CurrentPhase)), WorkerID: value(a.WorkerID), LeaseGeneration: valueU64(a.LeaseGeneration), RuntimeVersion: value(a.RuntimeVersion), RunCompatibilityHash: value(a.RunCompatibilityHash), CheckpointCompatibilityHash: value(a.CheckpointCompatibilityHash), ExecutingWorkerFingerprint: value(a.ExecutingWorkerFingerprint), TraceID: value(a.TraceID), OperationID: value(a.OperationID), RetryCount: int(valueU(a.RetryCount)), FailoverCount: int(valueU(a.FailoverCount)), FailureCode: value(a.FailureCode), FailureMessage: value(a.FailureMessageRedacted), UsageQuality: v1.DataQuality(value(a.UsageQuality)), TraceQuality: v1.DataQuality(value(a.TraceQuality)), StartedAt: a.StartedAt, FinishedAt: a.FinishedAt, ResourceMeta: v1.ResourceMeta{Availability: v1.AvailabilityAvailable, DataQuality: v1.DataQuality(quality)}})
	}
	return v1.AttemptsRes{Items: items, Page: v1.PageMeta{Page: p.Page, PageSize: p.PageSize, Total: total}}, nil
}

func (s *RuntimeService) ListCheckpoints(ctx context.Context, runID string, p v1.PageRequest) (v1.CheckpointsRes, error) {
	run, err := s.Store.GetRuntimeRun(ctx, runID)
	if err != nil {
		return v1.CheckpointsRes{}, MapRecoveryError(err)
	}
	if err = AuthorizeRun(ctx, run.UserID); err != nil {
		return v1.CheckpointsRes{}, err
	}
	if p.Page < 1 {
		p.Page = 1
	}
	if p.PageSize <= 0 {
		p.PageSize = 50
	}
	if p.PageSize > 100 {
		p.PageSize = 100
	}
	var rows []mysql.WorkflowCheckpoint
	q := s.Store.DB().WithContext(ctx).Select("id,run_id,checkpoint_key,payload_sha256,runtime_version,runtime_compatibility_hash,lease_generation,committed_at,expires_at,created_at").Where("run_id = ?", runID).Order("created_at DESC")
	var total int64
	q.Model(&mysql.WorkflowCheckpoint{}).Count(&total)
	q.Offset((p.Page - 1) * p.PageSize).Limit(p.PageSize).Find(&rows)
	items := make([]v1.CheckpointDTO, 0, len(rows))
	for _, c := range rows {
		items = append(items, v1.CheckpointDTO{CheckpointID: c.ID, CheckpointKey: c.CheckpointKey, PayloadSHA256: value(c.PayloadSHA256), RuntimeVersion: value(c.RuntimeVersion), RuntimeCompatibilityHash: value(c.RuntimeCompatibilityHash), LeaseGeneration: valueU64(c.LeaseGeneration), State: "committed", CommittedAt: c.CommittedAt, ExpiresAt: c.ExpiresAt, CreatedAt: c.CreatedAt, ResourceMeta: v1.ResourceMeta{Availability: v1.AvailabilityAvailable, DataQuality: v1.DataQualityComplete}})
	}
	return v1.CheckpointsRes{Items: items, Page: v1.PageMeta{Page: p.Page, PageSize: p.PageSize, Total: total}}, nil
}

func (s *RuntimeService) TailRuntimeEvents(ctx context.Context, runID string, afterSeq int64, send func(v1.RuntimeEventDTO) error) error {
	run, err := s.Store.GetRuntimeRun(ctx, runID)
	if err != nil {
		return MapRecoveryError(err)
	}
	if err = AuthorizeRun(ctx, run.UserID); err != nil {
		return err
	}
	cursor := afterSeq
	for {
		events, err := s.Store.ListEventsAfter(ctx, runID, cursor)
		if err != nil {
			return err
		}
		for _, e := range events {
			if e.ID <= cursor {
				continue
			}
			var row mysql.WorkflowEvent
			if err := s.Store.DB().WithContext(ctx).Where("run_id=? AND seq=?", runID, e.ID).First(&row).Error; err != nil {
				return err
			}
			if err := send(MapRuntimeEvent(row)); err != nil {
				return err
			}
			cursor = e.ID
			if isTailStop(e.Type, e.Payload) {
				return nil
			}
		}
		if len(events) == 0 && isTerminalRunStatus(run.Status) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func isTerminalRunStatus(status string) bool {
	return status == workflow.RunStatusSucceeded || status == workflow.RunStatusSuccess || status == workflow.RunStatusFailed || status == workflow.RunStatusCanceled
}

func isTailStop(t string, payload any) bool {
	if t == workflow.EventRunCompleted || t == workflow.EventRunParked || t == workflow.EventOperationSucceeded || t == workflow.EventOperationFailed || t == workflow.EventOperationCanceled {
		return true
	}
	if t == workflow.EventRunFailed {
		m, _ := payload.(map[string]any)
		d, _ := m["data"].(map[string]any)
		r, _ := d["retryable"].(bool)
		return !r
	}
	return false
}
