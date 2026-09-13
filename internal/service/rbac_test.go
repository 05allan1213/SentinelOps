package service_test

import (
	"context"
	"errors"
	"testing"

	"SentinelOps/internal/ai/policy"
	authsvc "SentinelOps/internal/service/auth"
	chatsvc "SentinelOps/internal/service/chat"
	eventsvc "SentinelOps/internal/service/event"
	knowledgesvc "SentinelOps/internal/service/knowledge"
	ragevalsvc "SentinelOps/internal/service/rageval"
	reportsvc "SentinelOps/internal/service/report"
	settingssvc "SentinelOps/internal/service/settings"
	subsvc "SentinelOps/internal/service/subscription"
	termsvc "SentinelOps/internal/service/term_mapping"
	tracesvc "SentinelOps/internal/service/trace"
)

func TestServiceLayerRechecksScope(t *testing.T) {
	t.Parallel()
	ctx := policy.WithIdentity(context.Background(), policy.DisabledIdentity())
	tests := map[string]func() error{
		"auth register": func() error {
			_, _, _, _, err := authsvc.Register(ctx, "blocked", "blocked-password", "")
			return err
		},
		"chat rollback": func() error {
			_, err := chatsvc.RollbackSession(ctx, "blocked-session", 0)
			return err
		},
		"event create": func() error {
			_, err := eventsvc.Create(ctx, "blocked", "", "medium", "", "", 0)
			return err
		},
		"knowledge create": func() error {
			_, err := knowledgesvc.CreateBase(ctx, "blocked", "")
			return err
		},
		"rageval feedback": func() error {
			return ragevalsvc.SubmitFeedback(ctx, "blocked-session", policy.DisabledUserID, 0, 1, nil)
		},
		"report create": func() error {
			_, err := reportsvc.CreateReport(ctx, "blocked", "", "custom")
			return err
		},
		"settings save": func() error {
			return settingssvc.SaveGeneral(ctx, "blocked", false)
		},
		"subscription create": func() error {
			_, err := subsvc.Create(ctx, "blocked", "https://invalid.example", "rss", "")
			return err
		},
		"term mapping create": func() error {
			_, err := termsvc.CreateTermMapping(ctx, "blocked", "blocked", 0, false)
			return err
		},
		"trace delete": func() error {
			_, err := tracesvc.NewService().BatchDelete(ctx, []string{"blocked"})
			return err
		},
	}
	for name, call := range tests {
		if err := call(); !errors.Is(err, policy.ErrForbidden) {
			t.Errorf("%s reached its write dependency: %v", name, err)
		}
	}
}
