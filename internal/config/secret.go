package config

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// SecretRef 是可持久化的 Secret 引用，不包含解析后的明文。
type SecretRef string

// Validate 只接受显式 env/file 引用，避免把明文误当作引用保存。
func (r SecretRef) Validate() error {
	value := string(r)
	prefix, target, ok := strings.Cut(value, ":")
	if !ok || strings.TrimSpace(target) == "" {
		return fmt.Errorf("secret reference must use env:<name> or file:<path>")
	}
	switch prefix {
	case "env":
		if strings.ContainsAny(target, "=/\\\x00\r\n\t ") {
			return fmt.Errorf("invalid environment variable name")
		}
	case "file":
		if strings.ContainsRune(target, '\x00') {
			return fmt.Errorf("invalid secret file path")
		}
	default:
		return fmt.Errorf("unsupported secret reference scheme %q", prefix)
	}
	return nil
}

// SecretResolver 在显式调用点即时解析 SecretRef。实现必须为每次调用返回可独立
// 清零的临时字节，不得缓存或复用解析值。
type SecretResolver interface {
	Resolve(context.Context, SecretRef) ([]byte, error)
}

// EnvironmentResolver 解析进程环境或本地 Secret 文件引用。
type EnvironmentResolver struct {
	lookupEnv func(string) (string, bool)
	readFile  func(string) ([]byte, error)
}

// NewEnvironmentResolver 创建不缓存解析值的默认 Resolver。
func NewEnvironmentResolver() SecretResolver {
	return &EnvironmentResolver{lookupEnv: os.LookupEnv, readFile: os.ReadFile}
}

// Resolve 返回独立临时字节；UseSecret 会在调用结束时清零。
func (r *EnvironmentResolver) Resolve(_ context.Context, ref SecretRef) ([]byte, error) {
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	prefix, target, _ := strings.Cut(string(ref), ":")
	switch prefix {
	case "env":
		value, ok := r.lookupEnv(target)
		if !ok {
			return nil, fmt.Errorf("secret environment variable is not set")
		}
		return []byte(value), nil
	case "file":
		value, err := r.readFile(filepath.Clean(target))
		if err != nil {
			return nil, fmt.Errorf("read secret file: %w", err)
		}
		return bytes.TrimRight(value, "\r\n"), nil
	default:
		return nil, fmt.Errorf("unsupported secret reference")
	}
}

var (
	resolverMu      sync.RWMutex
	currentResolver SecretResolver = NewEnvironmentResolver()
)

// SetSecretResolver 注入进程内唯一 Resolver；后续所有 Secret 调用点复用该实例。
func SetSecretResolver(resolver SecretResolver) {
	resolverMu.Lock()
	defer resolverMu.Unlock()
	currentResolver = resolver
}

// UseSecret 将解析值限定在回调内，并在回调结束后清零临时字节。
func UseSecret(ctx context.Context, ref SecretRef, use func([]byte) error) error {
	if use == nil {
		return fmt.Errorf("secret consumer is required")
	}
	resolverMu.RLock()
	resolver := currentResolver
	resolverMu.RUnlock()
	if resolver == nil {
		return fmt.Errorf("secret resolver has not been configured")
	}
	value, err := resolver.Resolve(ctx, ref)
	if err != nil {
		return err
	}
	defer clear(value)
	if len(value) == 0 {
		return fmt.Errorf("resolved secret is empty")
	}
	return use(value)
}
