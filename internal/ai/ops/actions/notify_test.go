package actions

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"SentinelOps/internal/ai/effects"
	appconfig "SentinelOps/internal/config"
)

type fixture24ActionResolver struct {
	value []byte
	ref   appconfig.SecretRef
}

func (r *fixture24ActionResolver) Resolve(_ context.Context, ref appconfig.SecretRef) ([]byte, error) {
	if ref != r.ref {
		return nil, fmt.Errorf("unexpected SecretRef")
	}
	r.value = []byte("phase24-short-lived-token")
	return r.value, nil
}

func TestConfigRejectsPlaintextSMTPParameter(t *testing.T) {
	_, err := (&EmailAction{}).Execute(effects.WithLegacyMutationContext(context.Background()), map[string]string{
		"smtp_host": "smtp.example.test",
		"smtp_pass": "plaintext-is-forbidden",
	})
	if err == nil || !strings.Contains(err.Error(), "smtp_pass") {
		t.Fatalf("Execute() error = %v, want plaintext SMTP parameter rejection", err)
	}
}

func TestExternalEffectEphemeralSecretReachesWebhookEndpoint(t *testing.T) {
	var gotKey, gotAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		gotKey = request.Header.Get("Idempotency-Key")
		gotAuthorization = request.Header.Get("Authorization")
		w.Header().Set("X-Request-ID", "provider-request-phase24")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	oldConfig, _ := appconfig.Current()
	resolver := &fixture24ActionResolver{ref: "env:phase24_EFFECT_TOKEN"}
	appconfig.SetCurrent(&appconfig.Config{Secrets: appconfig.SecretReferences{Effect: resolver.ref}})
	appconfig.SetSecretResolver(resolver)
	defer func() {
		appconfig.SetCurrent(oldConfig)
		appconfig.SetSecretResolver(appconfig.NewEnvironmentResolver())
	}()
	result, err := (&WebhookOutAction{}).Execute(effects.WithLegacyMutationContext(context.Background()), map[string]string{
		"url": server.URL, "payload": `{"event":"phase24"}`, "method": http.MethodPost,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotKey != "" || gotAuthorization != "Bearer phase24-short-lived-token" || result.Output["external_reference"] != "provider-request-phase24" {
		t.Fatalf("key=%q authorization=%q result=%#v", gotKey, gotAuthorization, result)
	}
	for _, value := range resolver.value {
		if value != 0 {
			t.Fatal("resolved Effect Secret was not cleared after endpoint return")
		}
	}
}

func TestExternalEffectUnknownErrorDoesNotExposeResolvedSecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()
	oldConfig, _ := appconfig.Current()
	resolver := &fixture24ActionResolver{ref: "env:phase24_UNKNOWN_TOKEN"}
	appconfig.SetCurrent(&appconfig.Config{Secrets: appconfig.SecretReferences{Effect: resolver.ref}})
	appconfig.SetSecretResolver(resolver)
	defer func() {
		appconfig.SetCurrent(oldConfig)
		appconfig.SetSecretResolver(appconfig.NewEnvironmentResolver())
	}()
	_, err := (&WebhookOutAction{}).Execute(effects.WithLegacyMutationContext(context.Background()), map[string]string{"url": url, "payload": `{}`})
	if err == nil || !strings.Contains(err.Error(), "result unknown") || strings.Contains(err.Error(), "phase24-short-lived-token") {
		t.Fatalf("error=%v", err)
	}
	var invocationErr *effects.InvocationError
	if !errors.As(err, &invocationErr) || invocationErr.Class != effects.InvocationUnknown {
		t.Fatalf("classification=%#v error=%v", invocationErr, err)
	}
}
