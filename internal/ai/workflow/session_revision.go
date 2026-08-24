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
