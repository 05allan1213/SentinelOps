// Package bootstrap 负责按角色编排 SentinelOps 既有初始化能力。
package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"SentinelOps/internal/ai/retrieval"
	airuntime "SentinelOps/internal/ai/runtime"
	appconfig "SentinelOps/internal/config"
	dao "SentinelOps/internal/dao/mysql"
	authpkg "SentinelOps/utility/auth"
)

// Role 是同一二进制支持的有限启动角色。
type Role string

const (
	// RoleAPI 只绑定 HTTP，不启动后台 Worker。
	RoleAPI Role = "api"
	// RoleWorker 只启动后台 Worker，不绑定 HTTP。
	RoleWorker Role = "worker"
	// RoleAll 同时启动 API 与 Worker，仅用于开发环境。
	RoleAll Role = "all"
)

// Options 描述启动角色、配置目录与唯一 Secret Resolver。
type Options struct {
	Role      Role
	ConfigDir string
	Resolver  appconfig.SecretResolver
}

type dependencies struct {
	loadConfig      func(string) (*appconfig.Config, string, error)
	initDatabase    func(context.Context, []byte) error
	initRuntime     func(context.Context) error
	shutdownRuntime func(context.Context) error
	initAuth        func([]byte) error
	seedAdmin       func(context.Context, []byte) error
	warmUp          func(context.Context) error
	bindAPI         func(context.Context) error
	startWorker     func(context.Context) error
	serveAPI        func(context.Context) error
	waitWorker      func(context.Context) error
}

// ParseRole 解析第一个位置参数；未提供时使用环境变量，仍未提供则默认为开发 all。
func ParseRole(args []string, lookupEnv func(string) (string, bool)) (Role, error) {
	value := ""
	if len(args) > 0 {
		value = strings.TrimSpace(args[0])
	}
	if value == "" && lookupEnv != nil {
		value, _ = lookupEnv("SENTINELOPS_ROLE")
		value = strings.TrimSpace(value)
	}
	if value == "" {
		value = string(RoleAll)
	}
	role := Role(value)
	switch role {
	case RoleAPI, RoleWorker, RoleAll:
		return role, nil
	default:
		return "", fmt.Errorf("unsupported bootstrap role %q", value)
	}
}

// Run 加载完整配置后按显式角色启动既有依赖。
func Run(ctx context.Context, options Options) error {
	return run(ctx, options, defaultDependencies())
}

func defaultDependencies() dependencies {
	return dependencies{
		loadConfig:      appconfig.LoadDirectory,
		initDatabase:    dao.InitWithDSN,
		initRuntime:     initializeSharedRuntime,
		shutdownRuntime: shutdownSharedRuntime,
		initAuth:        authpkg.Init,
		seedAdmin:       dao.SeedAdmin,
		warmUp:          retrieval.WarmUp,
		bindAPI:         bindAPI,
		startWorker:     startWorker,
		serveAPI:        serveAPI,
		waitWorker:      waitWorker,
	}
}

func run(ctx context.Context, options Options, deps dependencies) (runErr error) {
	if deps.loadConfig == nil {
		return fmt.Errorf("configuration loader is required")
	}
	role := options.Role
	if role == "" {
		role = RoleAll
	}
	switch role {
	case RoleAPI, RoleWorker, RoleAll:
	default:
		return fmt.Errorf("unsupported bootstrap role %q", role)
	}
	configDir := options.ConfigDir
	if configDir == "" {
		configDir = "manifest/config"
	}
	cfg, _, err := deps.loadConfig(configDir)
	if err != nil {
		return fmt.Errorf("load application configuration: %w", err)
	}
	if cfg == nil {
		return fmt.Errorf("configuration loader returned nil")
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate application configuration: %w", err)
	}
	if role == RoleAll && cfg.App.Environment != "development" {
		return fmt.Errorf("bootstrap role all is development-only")
	}
	if cfg.AgentRuntime.AcceptNewRuns && !cfg.AgentRuntime.Enabled {
		return fmt.Errorf("durable accept-new-runs requires agent runtime enabled")
	}
	resolver := options.Resolver
	if resolver == nil {
		resolver = appconfig.NewEnvironmentResolver()
	}
	appconfig.SetSecretResolver(resolver)

	production := cfg.App.Environment == "production"
	if production && cfg.AgentRuntime.Enabled {
		if err := airuntime.ValidateCurrentRuntimeVersion(); err != nil {
			return fmt.Errorf("validate durable runtime version: %w", err)
		}
	}
	if err := validateProductionSecrets(ctx, cfg, role, production); err != nil {
		return err
	}

	databaseReady := false
	if cfg.Database.MySQL.DSNRef != "" {
		if deps.initDatabase == nil {
			return fmt.Errorf("database initializer is required")
		}
		if err := appconfig.UseSecret(ctx, cfg.Database.MySQL.DSNRef, func(dsn []byte) error {
			return deps.initDatabase(ctx, dsn)
		}); err != nil {
			return fmt.Errorf("initialize database: %w", err)
		}
		databaseReady = true
	} else if production {
		return fmt.Errorf("production database reference is required")
	}

	if role == RoleAPI || role == RoleAll {
		if cfg.Auth.JWT.Enabled {
			if deps.initAuth == nil {
				return fmt.Errorf("JWT initializer is required")
			}
			if err := appconfig.UseSecret(ctx, cfg.Auth.JWT.SecretRef, deps.initAuth); err != nil {
				return fmt.Errorf("initialize JWT: %w", err)
			}
		}
		if databaseReady && cfg.Auth.Seed.AdminPasswordRef != "" {
			if deps.seedAdmin == nil {
				return fmt.Errorf("admin seeder is required")
			}
			if err := appconfig.UseSecret(ctx, cfg.Auth.Seed.AdminPasswordRef, func(password []byte) error {
				return deps.seedAdmin(ctx, password)
			}); err != nil {
				return fmt.Errorf("seed admin: %w", err)
			}
		}
	}
	if databaseReady && deps.initRuntime != nil {
		if err := deps.initRuntime(ctx); err != nil {
			return fmt.Errorf("initialize shared runtime: %w", err)
		}
		if deps.shutdownRuntime != nil {
			shutdownTimeout := 10 * time.Second
			if cfg.Observability.Langfuse.ShutdownTimeoutMS > 0 {
				shutdownTimeout = time.Duration(cfg.Observability.Langfuse.ShutdownTimeoutMS) * time.Millisecond
			}
			defer func() {
				shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
				defer cancel()
				runErr = errors.Join(runErr, deps.shutdownRuntime(shutdownCtx))
			}()
		}
	}
	if deps.warmUp == nil {
		return fmt.Errorf("retrieval warmup is required")
	}
	if err := deps.warmUp(ctx); err != nil {
		return fmt.Errorf("retrieval warmup: %w", err)
	}

	if role == RoleAPI || role == RoleAll {
		if deps.bindAPI == nil || deps.serveAPI == nil {
			return fmt.Errorf("API lifecycle hooks are required")
		}
		if err := deps.bindAPI(ctx); err != nil {
			return fmt.Errorf("bind API: %w", err)
		}
	}
	if role == RoleWorker || role == RoleAll {
		if deps.startWorker == nil {
			return fmt.Errorf("worker lifecycle hook is required")
		}
		if err := deps.startWorker(ctx); err != nil {
			return fmt.Errorf("start worker: %w", err)
		}
	}
	if role == RoleAPI || role == RoleAll {
		return deps.serveAPI(ctx)
	}
	if deps.waitWorker == nil {
		return fmt.Errorf("worker wait hook is required")
	}
	return deps.waitWorker(ctx)
}

func validateProductionSecrets(ctx context.Context, cfg *appconfig.Config, role Role, production bool) error {
	if !production {
		return nil
	}
	required := []struct {
		name string
		ref  appconfig.SecretRef
	}{
		{name: "database", ref: cfg.Database.MySQL.DSNRef},
	}
	if role == RoleAPI || role == RoleAll {
		if cfg.Auth.JWT.Enabled {
			required = append(required, struct {
				name string
				ref  appconfig.SecretRef
			}{name: "JWT", ref: cfg.Auth.JWT.SecretRef})
		}
		required = append(required, struct {
			name string
			ref  appconfig.SecretRef
		}{name: "admin", ref: cfg.Auth.Seed.AdminPasswordRef})
	}
	for providerName, provider := range cfg.Providers {
		required = append(required, struct {
			name string
			ref  appconfig.SecretRef
		}{name: "model provider " + providerName, ref: provider.SecretRef})
	}
	for name, ref := range map[string]appconfig.SecretRef{
		"MCP":    cfg.Secrets.MCPHeader,
		"Effect": cfg.Secrets.Effect,
	} {
		if ref != "" {
			required = append(required, struct {
				name string
				ref  appconfig.SecretRef
			}{name: name, ref: ref})
		}
	}
	if cfg.SOAR.Integrations.Email.SMTPHost != "" {
		required = append(required, struct {
			name string
			ref  appconfig.SecretRef
		}{name: "SMTP", ref: cfg.SOAR.Integrations.Email.SMTPPasswordRef})
	}
	for name, ref := range map[string]appconfig.SecretRef{
		"DingTalk webhook": cfg.SOAR.Integrations.DingTalk.WebhookRef,
		"WeCom webhook":    cfg.SOAR.Integrations.WeCom.WebhookRef,
	} {
		if ref != "" {
			required = append(required, struct {
				name string
				ref  appconfig.SecretRef
			}{name: name, ref: ref})
		}
	}
	for _, item := range required {
		if item.ref == "" {
			return fmt.Errorf("production %s secret reference is required", item.name)
		}
		if err := appconfig.UseSecret(ctx, item.ref, func(value []byte) error {
			if isDefaultSecret(value) {
				return fmt.Errorf("default secret is forbidden")
			}
			return nil
		}); err != nil {
			return fmt.Errorf("production %s secret validation failed: %w", item.name, err)
		}
	}
	return nil
}

func isDefaultSecret(value []byte) bool {
	normalized := bytes.ToLower(bytes.TrimSpace(value))
	defer clear(normalized)
	if len(normalized) == 0 {
		return true
	}
	for _, forbidden := range []string{
		"admin123",
		"change-me-in-production",
		"sentinelops-default-secret-change-in-prod",
		"sentinel123",
	} {
		if bytes.Equal(normalized, []byte(forbidden)) || bytes.Contains(normalized, []byte(":"+forbidden+"@")) {
			return true
		}
	}
	return false
}
