// Package evidence 定义 RAG 证据和授权 Scope 的稳定契约。
package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrInvalidEvidence = errors.New("invalid evidence reference")

// EvidenceRef 是一次检索返回的完整、可核对证据引用。
type EvidenceRef struct {
	EvidenceID     string    `json:"evidence_id"`
	SourceType     string    `json:"source_type"`
	SourceID       string    `json:"source_id"`
	BaseID         string    `json:"base_id"`
	DocumentID     string    `json:"document_id"`
	ChunkID        string    `json:"chunk_id"`
	SourceVersion  string    `json:"source_version"`
	ContentHash    string    `json:"content_hash"`
	AccessScope    string    `json:"access_scope"`
	IndexedVersion uint64    `json:"indexed_version,omitempty"`
	RetrievedAt    time.Time `json:"retrieved_at"`
	Quote          string    `json:"quote,omitempty"`
	ContentPreview string    `json:"content_preview,omitempty"`
	VectorScore    float64   `json:"vector_score"`
	RerankScore    float64   `json:"rerank_score"`
}

// Validate 检查序列化契约和稳定身份所需的不可变字段。
func (e EvidenceRef) Validate() error {
	for name, value := range map[string]string{
		"evidence_id": e.EvidenceID, "source_type": e.SourceType, "source_id": e.SourceID,
		"base_id": e.BaseID, "document_id": e.DocumentID, "chunk_id": e.ChunkID,
		"source_version": e.SourceVersion, "content_hash": e.ContentHash, "access_scope": e.AccessScope,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidEvidence, name)
		}
	}
	if e.RetrievedAt.IsZero() || (strings.TrimSpace(e.Quote) == "" && strings.TrimSpace(e.ContentPreview) == "") {
		return fmt.Errorf("%w: retrieved_at and quote/content_preview are required", ErrInvalidEvidence)
	}
	if len(e.ContentHash) != 64 {
		return fmt.Errorf("%w: content_hash must be sha256 hex", ErrInvalidEvidence)
	}
	if _, err := hex.DecodeString(e.ContentHash); err != nil {
		return fmt.Errorf("%w: content_hash must be sha256 hex: %v", ErrInvalidEvidence, err)
	}
	if e.VectorScore < 0 || e.RerankScore < 0 {
		return fmt.Errorf("%w: scores must be non-negative", ErrInvalidEvidence)
	}
	return nil
}

// UnmarshalEvidenceRef 解码并验证 EvidenceRef，拒绝缺字段或身份不一致的结果。
func UnmarshalEvidenceRef(data []byte) (EvidenceRef, error) {
	var ref EvidenceRef
	if err := json.Unmarshal(data, &ref); err != nil {
		return EvidenceRef{}, fmt.Errorf("%w: %v", ErrInvalidEvidence, err)
	}
	if err := ref.Validate(); err != nil {
		return EvidenceRef{}, err
	}
	if want := StableEvidenceID(ref.SourceType, ref.SourceID, ref.BaseID, ref.DocumentID, ref.ChunkID, ref.SourceVersion, ref.ContentHash); ref.EvidenceID != want {
		return EvidenceRef{}, fmt.Errorf("%w: evidence_id does not match source identity", ErrInvalidEvidence)
	}
	return ref, nil
}

// StableEvidenceID 由来源、版本和内容 hash 计算，不包含检索时间或分数。
func StableEvidenceID(sourceType, sourceID, baseID, documentID, chunkID, sourceVersion, contentHash string) string {
	canonical := strings.Join([]string{sourceType, sourceID, baseID, documentID, chunkID, sourceVersion, contentHash}, "\x00")
	sum := sha256.Sum256([]byte(canonical))
	return "evidence-v1:" + hex.EncodeToString(sum[:])
}
