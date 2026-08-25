// Package limiter 提供按 provider-qualified Catalog Ref 隔离的进程级模型限流。
package limiter

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/time/rate"
)

// Settings 是模型候选共享的 QPS 与 burst 配置。
type Settings struct {
	QPS   float64
	Burst int
}

// Registry 按 Catalog Ref 复用唯一 Token Bucket。
type Registry struct {
	settings Settings
	mu       sync.Mutex
	entries  map[string]*rate.Limiter
}

// NewRegistry 创建候选限流 registry。
func NewRegistry(settings Settings) (*Registry, error) {
	if settings.QPS <= 0 || settings.Burst <= 0 {
		return nil, fmt.Errorf("model limiter requires positive qps and burst")
	}
	return &Registry{settings: settings, entries: make(map[string]*rate.Limiter)}, nil
}

func (r *Registry) candidate(catalogRef string) (*rate.Limiter, error) {
	if r == nil || strings.TrimSpace(catalogRef) == "" {
		return nil, fmt.Errorf("model limiter Catalog Ref is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing := r.entries[catalogRef]; existing != nil {
		return existing, nil
	}
	created := rate.NewLimiter(rate.Limit(r.settings.QPS), r.settings.Burst)
	r.entries[catalogRef] = created
	return created, nil
}

// Wait 等待指定候选的令牌。
func (r *Registry) Wait(ctx context.Context, catalogRef string) error {
	candidate, err := r.candidate(catalogRef)
	if err != nil {
		return err
	}
	return candidate.Wait(ctx)
}

// Allow 非阻塞获取指定候选的令牌。
func (r *Registry) Allow(catalogRef string) bool {
	candidate, err := r.candidate(catalogRef)
	return err == nil && candidate.Allow()
}

var (
	globalMu          sync.RWMutex
	globalSettings    = Settings{QPS: 10, Burst: 20}
	globalRegistry, _ = NewRegistry(globalSettings)
)

// Configure 配置唯一进程级 registry；相同配置不会重置已有候选状态。
func Configure(settings Settings) error {
	globalMu.Lock()
	defer globalMu.Unlock()
	if settings == globalSettings {
		return nil
	}
	registry, err := NewRegistry(settings)
	if err != nil {
		return err
	}
	globalSettings = settings
	globalRegistry = registry
	return nil
}

// Wait 等待全局 registry 中指定候选的令牌。
func Wait(ctx context.Context, catalogRef string) error {
	globalMu.RLock()
	registry := globalRegistry
	globalMu.RUnlock()
	return registry.Wait(ctx, catalogRef)
}

// Allow 非阻塞获取全局 registry 中指定候选的令牌。
func Allow(catalogRef string) bool {
	globalMu.RLock()
	registry := globalRegistry
	globalMu.RUnlock()
	return registry.Allow(catalogRef)
}

// SetRate 保留既有运行时入口并更新唯一 registry。
func SetRate(requestsPerSecond float64, burst int) {
	_ = Configure(Settings{QPS: requestsPerSecond, Burst: burst})
}
