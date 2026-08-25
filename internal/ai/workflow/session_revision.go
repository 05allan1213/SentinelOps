package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/dao/mysql"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const SessionStateSchemaV1 = "fo/session-state/v1"

// BuildSessionRevisionPayload 根据已提交 Revision 构建下一版不可变会话状态。
// Query 和 Completion 只作为当前轮新增消息写入；调用方不得传入拼接后的历史。
func BuildSessionRevisionPayload(base json.RawMessage, nextRevision uint64, query, output string) (json.RawMessage, error) {
	if len(base) == 0 || !json.Valid(base) {
		return nil, fmt.Errorf("base session revision must be valid JSON")
	}
	if nextRevision == 0 {
		return nil, fmt.Errorf("next session revision must be greater than zero")
	}
	if strings.TrimSpace(query) == "" || strings.TrimSpace(output) == "" {
		return nil, fmt.Errorf("current query and output are required")
	}
	var state map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(base)))
	decoder.UseNumber()
	if err := decoder.Decode(&state); err != nil {
		return nil, fmt.Errorf("decode base session revision: %w", err)
	}
	if schema, _ := state["schema"].(string); schema != SessionStateSchemaV1 {
		return nil, fmt.Errorf("unsupported session state schema %q", schema)
	}
	if revision, ok := state["revision"].(json.Number); ok {
		previous, err := revision.Int64()
		if err != nil || previous < 0 || uint64(previous)+1 != nextRevision {
			return nil, fmt.Errorf("session revision must advance to %d", nextRevision)
		}
	}
	history, ok := state["history"].([]any)
	if !ok {
		history = make([]any, 0)
	}
	history = append(history,
		map[string]any{"role": "user", "content": query},
		map[string]any{"role": "assistant", "content": output},
	)
	state["schema"] = SessionStateSchemaV1
	state["revision"] = nextRevision
	state["history"] = history
	payload, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("marshal next session revision: %w", err)
	}
	return payload, nil
}

type revisionZeroPreference struct {
	OutputStyle   string   `json:"output_style,omitempty"`
	AnalysisDepth string   `json:"analysis_depth,omitempty"`
	FocusAreas    []string `json:"focus_areas,omitempty"`
	InferredNote  string   `json:"inferred_note,omitempty"`
}

type revisionZeroProvenance struct {
	Preference string `json:"preference"`
	History    string `json:"history"`
}

type revisionZeroState struct {
	Schema     string                  `json:"schema"`
	Revision   uint64                  `json:"revision"`
	Preference *revisionZeroPreference `json:"preference,omitempty"`
	Summary    string                  `json:"summary"`
	History    []any                   `json:"history"`
	Provenance revisionZeroProvenance  `json:"provenance"`
}

// BootstrapRevisionZero 在独立事务中以 insert-if-absent 建立并返回 Revision 0。
// P08 在 CreateRunWithSessionLock 的同一事务中直接复用 bootstrapRevisionZero。
func (s *GORMStore) BootstrapRevisionZero(ctx context.Context, sessionID, userID string) (*mysql.SessionStateRevision, error) {
	if err := policy.Authorize(ctx, policy.PermissionViewScoped, policy.Resource{OwnerID: strings.TrimSpace(userID)}); err != nil {
		return nil, err
	}
	var revision *mysql.SessionStateRevision
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		revision, err = bootstrapRevisionZero(ctx, tx, sessionID, userID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return revision, nil
}

func bootstrapRevisionZero(ctx context.Context, tx *gorm.DB, sessionID, userID string) (*mysql.SessionStateRevision, error) {
	sessionID = strings.TrimSpace(sessionID)
	userID = strings.TrimSpace(userID)
	if sessionID == "" {
		return nil, fmt.Errorf("revision zero session id is required")
	}
	if userID == "" {
		return nil, fmt.Errorf("revision zero user id is required")
	}

	state := revisionZeroState{
		Schema:   SessionStateSchemaV1,
		Revision: 0,
		Summary:  "",
		History:  make([]any, 0),
		Provenance: revisionZeroProvenance{
			Preference: "none",
			History:    "empty_no_durable_mysql_history",
		},
	}
	var preference mysql.UserPreference
	preferenceResult := tx.WithContext(ctx).Where("user_id = ?", userID).Limit(1).Find(&preference)
	if preferenceResult.Error != nil {
		return nil, fmt.Errorf("读取 Revision 0 MySQL preference: %w", preferenceResult.Error)
	}
	if preferenceResult.RowsAffected == 1 {
		focusAreas := make([]string, 0)
		if strings.TrimSpace(preference.FocusAreas) != "" {
			if err := json.Unmarshal([]byte(preference.FocusAreas), &focusAreas); err != nil {
				return nil, fmt.Errorf("解析 Revision 0 MySQL focus_areas: %w", err)
			}
		}
		state.Preference = &revisionZeroPreference{
			OutputStyle:   preference.OutputStyle,
			AnalysisDepth: preference.AnalysisDepth,
			FocusAreas:    focusAreas,
			InferredNote:  preference.InferredNote,
		}
		state.Provenance.Preference = "mysql.user_preferences"
	}

	stateJSON, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("序列化 Revision 0: %w", err)
	}
	candidate := mysql.SessionStateRevision{
		SessionID: sessionID,
		Revision:  0,
		StateJSON: string(stateJSON),
	}
	if err := tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "session_id"}, {Name: "revision"}},
		DoNothing: true,
	}).Create(&candidate).Error; err != nil {
		return nil, fmt.Errorf("插入 Revision 0: %w", err)
	}

	var revision mysql.SessionStateRevision
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("session_id = ? AND revision = 0", sessionID).
		First(&revision).Error; err != nil {
		return nil, fmt.Errorf("读取 Revision 0: %w", err)
	}
	return &revision, nil
}
