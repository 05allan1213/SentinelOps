// Package mysql AI 智能运维数据访问层
package mysql

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ---- Playbook ----

func ListPlaybooks(ctx context.Context) ([]OpsPlaybook, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}
	var list []OpsPlaybook
	return list, db.Order("created_at desc").Find(&list).Error
}

func CreatePlaybook(ctx context.Context, p *OpsPlaybook) error {
	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	db, err := DB(ctx)
	if err != nil {
		return err
	}
	return db.Create(p).Error
}

func UpdatePlaybook(ctx context.Context, p *OpsPlaybook) error {
	db, err := DB(ctx)
	if err != nil {
		return err
	}
	return db.Save(p).Error
}

func DeletePlaybook(ctx context.Context, id string) error {
	db, err := DB(ctx)
	if err != nil {
		return err
	}
	return db.Delete(&OpsPlaybook{}, "id = ?", id).Error
}

func GetPlaybook(ctx context.Context, id string) (*OpsPlaybook, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}
	var p OpsPlaybook
	return &p, db.First(&p, "id = ?", id).Error
}

// ---- Run ----

func CreateRun(ctx context.Context, r *OpsRun) error {
	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	db, err := DB(ctx)
	if err != nil {
		return err
	}
	return db.Create(r).Error
}

func UpdateRunStatus(ctx context.Context, runID, status, errMsg string) error {
	db, err := DB(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	updates := map[string]interface{}{"status": status, "error_msg": errMsg, "finished_at": now}
	// 计算耗时：从 started_at 到现在
	var run OpsRun
	if db.Select("started_at").Where("id = ?", runID).First(&run).Error == nil && !run.StartedAt.IsZero() {
		updates["duration_ms"] = now.Sub(run.StartedAt).Milliseconds()
	}
	return db.Model(&OpsRun{}).Where("id = ?", runID).Updates(updates).Error
}

func ListRuns(ctx context.Context, limit int) ([]OpsRun, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}
	var list []OpsRun
	if err := db.Order("created_at desc").Limit(limit).Find(&list).Error; err != nil {
		return nil, err
	}
	// 填充触发事件信息
	eventIDs := make([]string, 0, len(list))
	for _, r := range list {
		eventIDs = append(eventIDs, r.EventID)
	}
	var events []Event
	db.Unscoped().Select("id, title, severity").Where("id IN ?", eventIDs).Find(&events)
	eventMap := make(map[string]Event, len(events))
	for _, e := range events {
		eventMap[e.ID] = e
	}
	for i := range list {
		if ev, ok := eventMap[list[i].EventID]; ok {
			list[i].EventTitle = ev.Title
			list[i].EventSeverity = ev.Severity
		}
	}
	return list, nil
}

func GetRun(ctx context.Context, id string) (*OpsRun, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}
	var r OpsRun
	return &r, db.First(&r, "id = ?", id).Error
}

// ---- RunStep ----

func CreateRunStep(ctx context.Context, s *OpsRunStep) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	db, err := DB(ctx)
	if err != nil {
		return err
	}
	return db.Create(s).Error
}

func UpdateRunStep(ctx context.Context, s *OpsRunStep) error {
	db, err := DB(ctx)
	if err != nil {
		return err
	}
	return db.Save(s).Error
}

func GetRunSteps(ctx context.Context, runID string) ([]OpsRunStep, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}
	var list []OpsRunStep
	return list, db.Where("run_id = ?", runID).Order("step_order asc").Find(&list).Error
}

// ---- ProtectedAsset ----

func IsProtectedAsset(ctx context.Context, assetType, value string) (bool, error) {
	db, err := DB(ctx)
	if err != nil {
		return false, err
	}
	var count int64
	err = db.Model(&OpsProtectedAsset{}).Where("asset_type = ? AND value = ?", assetType, value).Count(&count).Error
	return count > 0, err
}

func CreateProtectedAsset(ctx context.Context, a *OpsProtectedAsset) error {
	db, err := DB(ctx)
	if err != nil {
		return err
	}
	return db.Create(a).Error
}

// ---- Stats ----

type OpsStats struct {
	TotalRuns   int64
	SuccessRuns int64
	FailedRuns  int64
}

func GetOpsStats(ctx context.Context) (OpsStats, error) {
	db, err := DB(ctx)
	if err != nil {
		return OpsStats{}, err
	}
	var s OpsStats
	db.Model(&OpsRun{}).Count(&s.TotalRuns)
	db.Model(&OpsRun{}).Where("status = 'success'").Count(&s.SuccessRuns)
	db.Model(&OpsRun{}).Where("status = 'failed'").Count(&s.FailedRuns)
	return s, nil
}

// ClearRuns 清空所有执行历史（含步骤明细）
func ClearRuns(ctx context.Context) error {
	db, err := DB(ctx)
	if err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("1 = 1").Delete(&OpsRunStep{}).Error; err != nil {
			return err
		}
		return tx.Where("1 = 1").Delete(&OpsRun{}).Error
	})
}

// DeleteRun 删除单条运维任务及其步骤明细
func DeleteRun(ctx context.Context, id string) error {
	db, err := DB(ctx)
	if err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("run_id = ?", id).Delete(&OpsRunStep{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(&OpsRun{}).Error
	})
}
