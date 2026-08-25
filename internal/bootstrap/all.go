package bootstrap

import (
	"context"
	"fmt"
	"strings"
	"time"

	"SentinelOps/internal/ai/rule"
	aitrace "SentinelOps/internal/ai/trace"
	appconfig "SentinelOps/internal/config"
	dao "SentinelOps/internal/dao/mysql"
	"SentinelOps/internal/service/knowledge"

	einocallbacks "github.com/cloudwego/eino/callbacks"
)

func initializeSharedRuntime(ctx context.Context) error {
	dao.SeedSettings(ctx)
	dao.SeedTermMappings(ctx)
	knowledge.EnsureDefaultBase(ctx)
	if err := dao.RegisterPlugin(aitrace.NewGORMPlugin()); err != nil {
		return err
	}
	rule.InitTermMappings(ctx)
	einocallbacks.AppendGlobalHandlers(aitrace.NewCallbackHandler())
	return initializeOptionalLangfuse(ctx)
}

func initializeOptionalLangfuse(ctx context.Context) error {
	config, err := appconfig.Current()
	if err != nil {
		return err
	}
	dynamicGate, err := dao.GetSetting(ctx, "observability.langfuse.enabled")
	if err != nil {
		return err
	}
	if !config.Observability.Langfuse.Enabled || !strings.EqualFold(strings.TrimSpace(dynamicGate), "true") {
		aitrace.InstallLangfuseRuntime(nil)
		return nil
	}
	langfuseConfig := config.Observability.Langfuse
	var runtime *aitrace.LangfuseRuntime
	err = appconfig.UseSecret(ctx, langfuseConfig.PublicKeyRef, func(publicKey []byte) error {
		return appconfig.UseSecret(ctx, langfuseConfig.SecretKeyRef, func(secretKey []byte) error {
			created, createErr := aitrace.NewLangfuseRuntime(ctx, aitrace.LangfuseOptions{
				StaticEnabled: true, DynamicEnabled: true,
				Host: langfuseConfig.Host, PublicKey: string(publicKey), SecretKey: string(secretKey),
				ServiceName: langfuseConfig.ServiceName, Environment: config.App.Environment,
				Version: "sentinelops-runtime", SampleRate: langfuseConfig.SampleRate,
				Timeout:                 time.Duration(langfuseConfig.TimeoutMS) * time.Millisecond,
				MaxAttributeValueLength: langfuseConfig.MaxAttributeValueLength,
				MaxSpanAttributeBytes:   langfuseConfig.MaxSpanAttributeBytes,
			})
			runtime = created
			return createErr
		})
	})
	if err != nil {
		return fmt.Errorf("initialize optional Langfuse: %w", err)
	}
	aitrace.InstallLangfuseRuntime(runtime)
	einocallbacks.AppendGlobalHandlers(runtime.Handler())
	return nil
}

func shutdownSharedRuntime(ctx context.Context) error {
	return aitrace.ShutdownInstalledLangfuse(ctx)
}
