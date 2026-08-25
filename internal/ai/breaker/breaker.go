// Package breaker 保存 provider-qualified 模型候选的进程级健康状态。
package breaker

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ErrOpen 表示候选仍处于跨请求保护窗口。
var ErrOpen = errors.New("model candidate circuit breaker is open")

// Settings 是所有候选共享的 breaker 策略；状态仍按 Catalog Ref 隔离。
type Settings struct {
	FailureThreshold int
	OpenTimeout      time.Duration
}

// Registry 保存进程级候选健康状态。
type Registry struct {
	settings Settings
	mu       sync.Mutex
	entries  map[string]*Health
}

// Health 是单个 provider-qualified Catalog Ref 的并发安全健康状态。
type Health struct {
	settings Settings
	mu       sync.Mutex
	failures int
	openedAt time.Time
}

// NewRegistry 创建共享 registry。
func NewRegistry(settings Settings) *Registry {
	return &Registry{settings: settings, entries: make(map[string]*Health)}
}

// For 返回同一 Catalog Ref 的共享 Health。
func (r *Registry) For(catalogRef string) (*Health, error) {
	if r == nil || r.settings.FailureThreshold <= 0 || r.settings.OpenTimeout <= 0 {
		return nil, fmt.Errorf("breaker settings require positive threshold and timeout")
	}
	if strings.TrimSpace(catalogRef) == "" {
		return nil, fmt.Errorf("breaker Catalog Ref is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing := r.entries[catalogRef]; existing != nil {
		return existing, nil
	}
	health := &Health{settings: r.settings}
	r.entries[catalogRef] = health
	return health, nil
}

// Allow 在 open timeout 内拒绝候选，超时后允许新的物理调用。
func (h *Health) Allow() error {
	if h == nil {
		return fmt.Errorf("breaker health is required")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.openedAt.IsZero() {
		return nil
	}
	if time.Since(h.openedAt) < h.settings.OpenTimeout {
		return ErrOpen
	}
	h.failures = 0
	h.openedAt = time.Time{}
	return nil
}

// Success 清除连续失败。
func (h *Health) Success() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.failures = 0
	h.openedAt = time.Time{}
}

// Failure 记录一次完整物理调用失败。
func (h *Health) Failure() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.failures++
	if h.failures >= h.settings.FailureThreshold && h.openedAt.IsZero() {
		h.openedAt = time.Now()
	}
}
