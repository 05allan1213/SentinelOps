package mysql

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// RequiredSchemaVersion 是当前应用可接受的 goose Schema 版本。
const RequiredSchemaVersion int64 = 9

// ErrSchemaVersionMismatch 标识数据库 Schema 尚未迁移到应用要求的精确版本。
var ErrSchemaVersionMismatch = errors.New("schema version mismatch")

// SchemaVersionMismatchError 保存脱敏的版本差异；Cause 仅包含数据库错误，不包含 DSN。
type SchemaVersionMismatchError struct {
	Have    int64
	Want    int64
	Applied bool
	Cause   error
}

func (e *SchemaVersionMismatchError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%v: require goose version %d: %v", ErrSchemaVersionMismatch, e.Want, e.Cause)
	}
	return fmt.Sprintf("%v: have version %d applied=%t, want version %d", ErrSchemaVersionMismatch, e.Have, e.Applied, e.Want)
}

func (e *SchemaVersionMismatchError) Unwrap() error { return ErrSchemaVersionMismatch }

// CheckSchemaVersion 只读取 goose 版本表，不创建、升级或回滚任何 Schema 对象。
func CheckSchemaVersion(ctx context.Context, db *gorm.DB) error {
	var row struct {
		VersionID int64 `gorm:"column:version_id"`
		IsApplied bool  `gorm:"column:is_applied"`
	}
	result := db.WithContext(ctx).Raw(`SELECT version_id, is_applied
		FROM goose_db_version ORDER BY id DESC LIMIT 1`).Scan(&row)
	if result.Error != nil {
		return &SchemaVersionMismatchError{Want: RequiredSchemaVersion, Cause: result.Error}
	}
	if result.RowsAffected != 1 || row.VersionID != RequiredSchemaVersion || !row.IsApplied {
		return &SchemaVersionMismatchError{
			Have: row.VersionID, Want: RequiredSchemaVersion, Applied: row.IsApplied,
		}
	}
	return nil
}
