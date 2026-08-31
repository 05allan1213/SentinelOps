package runtime

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/dao/mysql"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	WorkerStatusIdle        = "idle"
	WorkerStatusRunning     = "running"
	WorkerStatusDraining    = "draining"
	WorkerStatusStale       = "stale"
	WorkerStatusUnavailable = "unavailable"
)

// ObservedRuntimeComponent 是 Worker 亲自观测到的 MCP/Skill 脱敏身份。
// 字段刻意不包含 URL、Header、SecretRef、路径或连接句柄。
type ObservedRuntimeComponent struct {
	Name       string `json:"name"`
	Version    string `json:"version,omitempty"`
	Hash       string `json:"hash,omitempty"`
	Validation string `json:"validation,omitempty"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
}

// WorkerObservation 是 runtime_worker_snapshots 的唯一写入契约。
type WorkerObservation struct {
	WorkerID                 string
	HeartbeatAt              time.Time
	RuntimeVersion           string
	RuntimeCompatibilityHash string
	ConfiguredCatalogHash    string
	ObservedMCP              []ObservedRuntimeComponent
	ObservedSkill            []ObservedRuntimeComponent
	ActiveRunID              string
	ActiveGeneration         uint64
	Status                   string
	LastError                string
}

// PersistWorkerSnapshot 创建或替换 Worker 的完整脱敏观测身份。
func PersistWorkerSnapshot(ctx context.Context, db *gorm.DB, observation WorkerObservation) error {
	if db == nil {
		return fmt.Errorf("worker snapshot database is required")
	}
	record, err := workerSnapshotRecord(observation)
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "worker_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"heartbeat_at", "runtime_version", "runtime_compatibility_hash", "configured_catalog_hash",
			"observed_mcp_json", "observed_skill_json", "active_run_id", "active_generation",
			"status", "last_error_redacted", "updated_at",
		}),
	}).Create(&record).Error
}

// HeartbeatWorkerSnapshot 刷新现有观测；若启动行尚未落库则以同一安全契约补建。
func HeartbeatWorkerSnapshot(ctx context.Context, db *gorm.DB, observation WorkerObservation) error {
	return PersistWorkerSnapshot(ctx, db, observation)
}

// ClassifyObservedWorker 只按持久化 heartbeat 分类；缺失观测绝不推断健康。
func ClassifyObservedWorker(now time.Time, heartbeatAt *time.Time, leaseDuration time.Duration) string {
	if heartbeatAt == nil || heartbeatAt.IsZero() || leaseDuration <= 0 {
		return WorkerStatusUnavailable
	}
	if !now.Before(heartbeatAt.Add(leaseDuration)) {
		return WorkerStatusStale
	}
	return WorkerStatusIdle
}

func workerSnapshotRecord(observation WorkerObservation) (mysql.RuntimeWorkerSnapshot, error) {
	observation.WorkerID = strings.TrimSpace(observation.WorkerID)
	if observation.WorkerID == "" || len(observation.WorkerID) > 128 {
		return mysql.RuntimeWorkerSnapshot{}, fmt.Errorf("worker snapshot owner must contain 1 to 128 bytes")
	}
	if observation.HeartbeatAt.IsZero() {
		observation.HeartbeatAt = time.Now()
	}
	if observation.Status == "" {
		observation.Status = WorkerStatusIdle
	}
	if !validWorkerStatus(observation.Status) || observation.Status == WorkerStatusStale {
		return mysql.RuntimeWorkerSnapshot{}, fmt.Errorf("worker cannot persist status %q", observation.Status)
	}
	for field, value := range map[string]string{
		"runtime_version": observation.RuntimeVersion, "runtime_compatibility_hash": observation.RuntimeCompatibilityHash,
		"configured_catalog_hash": observation.ConfiguredCatalogHash,
	} {
		if err := validateWorkerText(field, value); err != nil {
			return mysql.RuntimeWorkerSnapshot{}, err
		}
	}
	for field, value := range map[string]string{"runtime_compatibility_hash": observation.RuntimeCompatibilityHash, "configured_catalog_hash": observation.ConfiguredCatalogHash} {
		if value != "" && (len(value) != 64 || !isHexWorkerHash(value)) {
			return mysql.RuntimeWorkerSnapshot{}, fmt.Errorf("%s must be a SHA-256 digest", field)
		}
	}
	mcpJSON, err := encodeObservedComponents(observation.ObservedMCP)
	if err != nil {
		return mysql.RuntimeWorkerSnapshot{}, fmt.Errorf("encode observed MCP snapshot: %w", err)
	}
	skillJSON, err := encodeObservedComponents(observation.ObservedSkill)
	if err != nil {
		return mysql.RuntimeWorkerSnapshot{}, fmt.Errorf("encode observed Skill snapshot: %w", err)
	}
	now := observation.HeartbeatAt
	persistedAt := time.Now()
	lastError := normalizeWorkerError(observation.LastError)
	if err := validateWorkerText("last_error_redacted", lastError); err != nil {
		return mysql.RuntimeWorkerSnapshot{}, err
	}
	return mysql.RuntimeWorkerSnapshot{
		WorkerID: observation.WorkerID, HeartbeatAt: &now,
		RuntimeVersion:           nullableWorkerText(observation.RuntimeVersion),
		RuntimeCompatibilityHash: nullableWorkerText(observation.RuntimeCompatibilityHash),
		ConfiguredCatalogHash:    nullableWorkerText(observation.ConfiguredCatalogHash),
		ObservedMCPJSON:          &mcpJSON, ObservedSkillJSON: &skillJSON,
		ActiveRunID:       nullableWorkerText(observation.ActiveRunID),
		ActiveGeneration:  nullableWorkerGeneration(observation.ActiveRunID, observation.ActiveGeneration),
		Status:            nullableWorkerText(observation.Status),
		LastErrorRedacted: nullableWorkerText(lastError),
		CreatedAt:         &persistedAt, UpdatedAt: &persistedAt,
	}, nil
}

func encodeObservedComponents(components []ObservedRuntimeComponent) (string, error) {
	redactor := policy.NewRedactor()
	clean := append([]ObservedRuntimeComponent(nil), components...)
	sort.Slice(clean, func(i, j int) bool {
		if clean[i].Name != clean[j].Name {
			return clean[i].Name < clean[j].Name
		}
		if clean[i].Version != clean[j].Version {
			return clean[i].Version < clean[j].Version
		}
		return clean[i].Hash < clean[j].Hash
	})
	for index := range clean {
		if index > 0 && strings.TrimSpace(clean[index-1].Name) == strings.TrimSpace(clean[index].Name) {
			return "", fmt.Errorf("duplicate observed component %q", clean[index].Name)
		}
		for field, value := range map[string]string{"name": clean[index].Name, "version": clean[index].Version, "hash": clean[index].Hash, "validation": clean[index].Validation, "status": clean[index].Status} {
			if err := validateWorkerText("observed_"+field, value); err != nil {
				return "", err
			}
		}
		clean[index].Name = strings.TrimSpace(clean[index].Name)
		clean[index].Version = strings.TrimSpace(redactor.RedactText(clean[index].Version))
		clean[index].Hash = strings.TrimSpace(clean[index].Hash)
		clean[index].Validation = strings.TrimSpace(clean[index].Validation)
		clean[index].Status = strings.TrimSpace(clean[index].Status)
		if clean[index].Name == "" {
			return "", fmt.Errorf("observed component name is required")
		}
		clean[index].Error = normalizeWorkerError(clean[index].Error)
		if err := validateWorkerText("observed_error", clean[index].Error); err != nil {
			return "", err
		}
		if clean[index].Hash != "" && (len(clean[index].Hash) != 64 || !isHexWorkerHash(clean[index].Hash)) {
			return "", fmt.Errorf("observed component hash must be a SHA-256 digest")
		}
		if !validObservedValidation(clean[index].Validation) || !validObservedStatus(clean[index].Status) {
			return "", fmt.Errorf("observed component %q has invalid validation/status", clean[index].Name)
		}
	}
	encoded, err := json.Marshal(clean)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func validateWorkerText(field, value string) error {
	value = strings.TrimSpace(value)
	if len(value) > 512 {
		return fmt.Errorf("%s is too long", field)
	}
	lower := strings.ToLower(value)
	for _, forbidden := range []string{"secretref", "secret_ref", "authorization", "bearer ", "password", "api_key", "apikey", "token=", "http://", "https://", "handle"} {
		if strings.Contains(lower, forbidden) {
			return fmt.Errorf("%s contains forbidden sensitive material", field)
		}
	}
	return nil
}

func normalizeWorkerError(value string) string {
	value = policy.NewRedactor().RedactText(value)
	lower := strings.ToLower(value)
	for _, forbidden := range []string{
		"secretref", "secret_ref", "secret-ref", "secret ref", "http://", "https://", "://", "authorization", "proxy_authorization", "header", "handle",
		"password", "passwd", "api_key", "apikey", "token", "credential", "credentials", "x-api-key", "bearer ",
	} {
		if strings.Contains(lower, forbidden) {
			return "[REDACTED]"
		}
	}
	for _, line := range strings.Split(value, "\n") {
		if workerHeaderPattern.MatchString(strings.TrimSpace(line)) {
			return "[REDACTED]"
		}
	}
	return value
}

var workerHeaderPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{1,63}:\s*\S+`)

func isHexWorkerHash(value string) bool { _, err := hex.DecodeString(value); return err == nil }

func validObservedValidation(value string) bool {
	switch value {
	case "", "valid", "invalid", "not_run", "unknown":
		return true
	default:
		return false
	}
}

func validObservedStatus(value string) bool {
	switch value {
	case "", "loaded", "ready", "error", "disabled", "not_observed", "unavailable":
		return true
	default:
		return false
	}
}

func validWorkerStatus(status string) bool {
	switch status {
	case WorkerStatusIdle, WorkerStatusRunning, WorkerStatusDraining, WorkerStatusStale, WorkerStatusUnavailable:
		return true
	default:
		return false
	}
}

func nullableWorkerText(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func nullableWorkerGeneration(runID string, generation uint64) *uint64 {
	if strings.TrimSpace(runID) == "" {
		return nil
	}
	return &generation
}
