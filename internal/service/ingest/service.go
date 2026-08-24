// Package ingest 告警入库服务。
//
// 设计思想：
//
//	将"去重判断 + 写库 + 异步索引"三步封装为单一 Ingest 函数，
//	所有接入格式（Webhook/CEF/LEEF/API Push）共用同一入库路径，
//	保证去重逻辑唯一，避免各格式分别实现导致的不一致。
//
// 主要流程：
//  1. 计算 DedupKey（SHA256），查询 events 表是否已存在
//  2. 重复告警直接返回已有 ID（is_new=false），不写库
//  3. 新告警写入 events 表，event_type 记录接入渠道
//  4. 异步触发向量索引（不阻塞 HTTP 响应）
package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"SentinelOps/internal/ai/ops/engine"
	dao "SentinelOps/internal/dao/mysql"
	"SentinelOps/internal/service/pipeline"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/google/uuid"
)

// Ingest 将归一化告警写入 events 表（去重），返回事件 ID 和是否为新事件。
// 重复告警（dedup_key 已存在）直接返回已有 ID，不重复写入。
func Ingest(ctx context.Context, alert *NormalizedAlert) (id string, isNew bool, err error) {
	dedupKey := alert.DedupKey()

	// 去重检查：查询失败时返回错误，避免数据库故障时重复写入
	existing, err := dao.GetEventByDedupKey(ctx, dedupKey)
	if err != nil {
		return "", false, fmt.Errorf("去重查询失败: %w", err)
	}
	if existing != nil {
		return existing.ID, false, nil
	}

	occurredAt := alert.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now()
	}

	// 构造扩展 metadata
	meta := map[string]string{"ingest_source": alert.IngestSource}
	for k, v := range alert.ExtraFields {
		meta[k] = v
	}
	metaJSON, _ := json.Marshal(meta)

	severity := alert.Severity
	if severity == "" {
		severity = "medium"
	}

	e := &dao.Event{
		ID:         uuid.New().String(),
		Title:      alert.Title,
		Content:    alert.Content,
		EventType:  alert.IngestSource, // 接入来源：webhook / cef / leef / api_push
		DedupKey:   dedupKey,
		Severity:   severity,
		Source:     alert.Source,
		Status:     "new",
		CVEID:      alert.CVEID,
		RiskScore:  pipeline.SeverityToRiskScore(severity),
		Metadata:   string(metaJSON),
		RawPayload: alert.RawPayload,
	}

	if err := dao.CreateEvent(ctx, e); err != nil {
		return "", false, err
	}

	// 异步向量索引
	pipeline.IndexDocumentsAsync(ctx, []dao.Event{*e})

	// 异步触发 SOAR Playbook 匹配
	go engine.TriggerForEvent(context.Background(), e)

	g.Log().Infof(ctx, "[ingest] 新告警入库 | id=%s | source=%s | severity=%s | title=%s",
		e.ID, e.Source, e.Severity, e.Title)

	return e.ID, true, nil
}
