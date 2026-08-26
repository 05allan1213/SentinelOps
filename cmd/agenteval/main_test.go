package main

import (
	"context"
	"net/http"
	"testing"

	"SentinelOps/internal/config"
)

func TestValidateCommandNeedsOnlyCases(t *testing.T) {
	opts, err := parseOptions([]string{"validate", "--cases", "cases.yaml"})
	if err != nil {
		t.Fatalf("parse validate options: %v", err)
	}
	if opts.command != "validate" || opts.casesPath != "cases.yaml" {
		t.Fatalf("options = %+v", opts)
	}
}

func TestRunCommandRequiresSecretReferenceAndBaseURL(t *testing.T) {
	for _, args := range [][]string{
		{"run", "--cases", "cases.yaml"},
		{"run", "--cases", "cases.yaml", "--base-url", "http://127.0.0.1:1"},
		{"run", "--cases", "cases.yaml", "--base-url", "http://127.0.0.1:1", "--dsn-ref", "plaintext"},
	} {
		if _, err := parseOptions(args); err == nil {
			t.Fatalf("unsafe/incomplete args accepted: %v", args)
		}
	}
	opts, err := parseOptions([]string{
		"run", "--cases", "cases.yaml", "--base-url", "http://127.0.0.1:1",
		"--dsn-ref", "env:EVAL_DSN", "--authorization-ref", "env:EVAL_TOKEN",
	})
	if err != nil {
		t.Fatalf("parse run options: %v", err)
	}
	if opts.dsnRef != "env:EVAL_DSN" || opts.authorizationRef != "env:EVAL_TOKEN" {
		t.Fatalf("options = %+v", opts)
	}
}

func TestAuthorizationReferenceIsClearedAfterUse(t *testing.T) {
	config.SetSecretResolver(staticSecretResolver{value: []byte("temporary-token")})
	t.Cleanup(func() { config.SetSecretResolver(config.NewEnvironmentResolver()) })

	var observed http.Header
	err := withAuthorization(context.Background(), "env:EVAL_TOKEN", func(header http.Header) error {
		observed = header
		if header.Get("Authorization") != "Bearer temporary-token" {
			t.Fatalf("authorization header = %q", header.Get("Authorization"))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("withAuthorization() error = %v", err)
	}
	if observed.Get("Authorization") != "" {
		t.Fatalf("authorization header was retained after use")
	}
}

type staticSecretResolver struct {
	value []byte
}

func (r staticSecretResolver) Resolve(context.Context, config.SecretRef) ([]byte, error) {
	return append([]byte(nil), r.value...), nil
}
