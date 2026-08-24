package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"SentinelOps/internal/dao/mysql"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// WorkflowRunInput 表示创建工作流运行记录所需的基础入参。
type WorkflowRunInput struct {
	ID           string
	WorkflowKey  string
	SessionID    string
	Status       string
	InputPayload string
	StartedAt    time.Time
}

// Store 定义工作流运行、事件流和检查点的持久化能力。
type Store interface {
	CreateRun(ctx context.Context, input WorkflowRunInput) (*mysql.WorkflowRun, error)
	AppendEvent(ctx context.Context, event StreamEvent) error
	ListEventsAfter(ctx context.Context, runID string, afterSeq int64) ([]StreamEvent, error)
	SaveCheckpoint(ctx context.Context, snapshot CheckpointSnapshot) error
	LatestCheckpoint(ctx context.Context, runID, checkpointKey string) (CheckpointSnapshot, error)
	FinishRun(ctx context.Context, runID, status, outputPayload, errorMessage string) error
}

// GORMStore 基于 GORM/MySQL 的工作流持久化实现。
type GORMStore struct {
	db *gorm.DB
}

// NewGORMStore 创建 GORM 工作流存储实例。
func NewGORMStore(db *gorm.DB) *GORMStore {
	return &GORMStore{db: db}
}

// BuildNextEvent 基于最后一个序号构造下一条可持久化流式事件。
func BuildNextEvent(runID string, lastSeq int64, eventType, payload string) StreamEvent {
	return StreamEvent{
		ID:        lastSeq + 1,
		RunID:     runID,
		Type:      eventType,
		Payload:   payload,
		CreatedAt: time.Now(),
	}
}

// MarshalCheckpointSnapshot 将检查点快照序列化为 JSON 字符串。
func MarshalCheckpointSnapshot(snapshot CheckpointSnapshot) (string, error) {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return "", fmt.Errorf("序列化工作流检查点快照: %w", err)
	}
	return string(data), nil
}

// UnmarshalCheckpointSnapshot 将 JSON 字符串反序列化为检查点快照。
func UnmarshalCheckpointSnapshot(payload string) (CheckpointSnapshot, error) {
	var snapshot CheckpointSnapshot
	if payload == "" {
		return snapshot, nil
	}
	if err := json.Unmarshal([]byte(payload), &snapshot); err != nil {
		return snapshot, fmt.Errorf("反序列化工作流检查点快照: %w", err)
	}
	return snapshot, nil
}

// CreateRun 创建一条工作流运行记录。
func (s *GORMStore) CreateRun(ctx context.Context, input WorkflowRunInput) (*mysql.WorkflowRun, error) {
	if input.ID == "" {
		input.ID = uuid.NewString()
	}
	if input.Status == "" {
		input.Status = RunStatusRunning
	}
	if input.StartedAt.IsZero() {
		input.StartedAt = time.Now()
	}

	run := &mysql.WorkflowRun{
		ID:           input.ID,
		WorkflowKey:  input.WorkflowKey,
		SessionID:    input.SessionID,
		Status:       input.Status,
		InputPayload: input.InputPayload,
		StartedAt:    input.StartedAt,
	}
	if err := s.db.WithContext(ctx).Create(run).Error; err != nil {
		return nil, fmt.Errorf("创建工作流运行记录: %w", err)
	}
	return run, nil
}

// AppendEvent 追加工作流事件；run_id + seq 重复时视为成功，避免断线重放重复报错。
func (s *GORMStore) AppendEvent(ctx context.Context, event StreamEvent) error {
	payload, err := marshalEventPayload(event.Payload)
	if err != nil {
		return err
	}

	model := mysql.WorkflowEvent{
		RunID:     event.RunID,
		Seq:       int(event.ID),
		EventType: event.Type,
		Payload:   payload,
	}
	if !event.CreatedAt.IsZero() {
		model.CreatedAt = event.CreatedAt
	}

	err = s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "run_id"}, {Name: "seq"}},
			DoNothing: true,
		}).
		Create(&model).Error
	if err != nil {
		return fmt.Errorf("追加工作流事件: %w", err)
	}
	return nil
}

// ListEventsAfter 查询指定运行中大于游标序号的事件，按 seq 升序返回。
func (s *GORMStore) ListEventsAfter(ctx context.Context, runID string, afterSeq int64) ([]StreamEvent, error) {
	var rows []mysql.WorkflowEvent
	if err := s.db.WithContext(ctx).
		Where("run_id = ? AND seq > ?", runID, afterSeq).
		Order("seq ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("查询工作流事件: %w", err)
	}

	events := make([]StreamEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, StreamEvent{
			ID:        int64(row.Seq),
			RunID:     row.RunID,
			Type:      row.EventType,
			Payload:   unmarshalEventPayload(row.Payload),
			CreatedAt: row.CreatedAt,
		})
	}
	return events, nil
}

// SaveCheckpoint 保存工作流可恢复检查点快照。
func (s *GORMStore) SaveCheckpoint(ctx context.Context, snapshot CheckpointSnapshot) error {
	payload, err := MarshalCheckpointSnapshot(snapshot)
	if err != nil {
		return err
	}

	checkpointKey := snapshot.CheckpointID
	if checkpointKey == "" {
		checkpointKey = "default"
	}

	model := mysql.WorkflowCheckpoint{
		ID:            uuid.NewString(),
		RunID:         snapshot.RunID,
		CheckpointKey: checkpointKey,
		SnapshotJSON:  payload,
	}
	if !snapshot.CreatedAt.IsZero() {
		model.CreatedAt = snapshot.CreatedAt
	}

	if err := s.db.WithContext(ctx).Create(&model).Error; err != nil {
		return fmt.Errorf("保存工作流检查点: %w", err)
	}
	return nil
}

// LatestCheckpoint 查询指定运行最新的检查点；checkpointKey 为空时返回该运行任意 key 的最新快照。
func (s *GORMStore) LatestCheckpoint(ctx context.Context, runID, checkpointKey string) (CheckpointSnapshot, error) {
	query := s.db.WithContext(ctx).Where("run_id = ?", runID)
	if checkpointKey != "" {
		query = query.Where("checkpoint_key = ?", checkpointKey)
	}

	var row mysql.WorkflowCheckpoint
	if err := query.Order("created_at DESC").First(&row).Error; err != nil {
		return CheckpointSnapshot{}, fmt.Errorf("查询最新工作流检查点: %w", err)
	}

	snapshot, err := UnmarshalCheckpointSnapshot(row.SnapshotJSON)
	if err != nil {
		return CheckpointSnapshot{}, err
	}
	return snapshot, nil
}

// FinishRun 更新工作流运行结束状态、输出、错误信息与结束时间。
func (s *GORMStore) FinishRun(ctx context.Context, runID, status, outputPayload, errorMessage string) error {
	now := time.Now()
	updates := map[string]any{
		"status":         status,
		"output_payload": outputPayload,
		"error_message":  errorMessage,
		"finished_at":    &now,
	}

	var run mysql.WorkflowRun
	if err := s.db.WithContext(ctx).Select("started_at").Where("id = ?", runID).First(&run).Error; err == nil && !run.StartedAt.IsZero() {
		updates["duration_ms"] = now.Sub(run.StartedAt).Milliseconds()
	}

	if err := s.db.WithContext(ctx).Model(&mysql.WorkflowRun{}).Where("id = ?", runID).Updates(updates).Error; err != nil {
		return fmt.Errorf("完成工作流运行记录: %w", err)
	}
	return nil
}

func marshalEventPayload(payload any) (string, error) {
	if payload == nil {
		return "", nil
	}
	if text, ok := payload.(string); ok {
		return text, nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("序列化工作流事件 payload: %w", err)
	}
	return string(data), nil
}

func unmarshalEventPayload(payload string) any {
	if payload == "" {
		return nil
	}
	var value any
	if err := json.Unmarshal([]byte(payload), &value); err != nil {
		return payload
	}
	return value
}
