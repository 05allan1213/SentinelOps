package main

import (
	"bytes"
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

func TestValidateCommandAcceptsDatasetDirectory(t *testing.T) {
	opts, err := parseOptions([]string{"validate", "--cases", "manifest/eval/cases"})
	if err != nil {
		t.Fatalf("parse Dataset validate options: %v", err)
	}
	if opts.casesPath != "manifest/eval/cases" || opts.repeat != 1 {
		t.Fatalf("options = %+v", opts)
	}
}

func TestRunCommandSupportsRepresentativeSamplesAndRepeat(t *testing.T) {
	opts, err := parseOptions([]string{
		"run", "--cases", "manifest/eval/cases", "--base-url", "http://127.0.0.1:1",
		"--dsn-ref", "env:EVAL_DSN", "--sample-per-category", "1", "--repeat", "3",
	})
	if err != nil {
		t.Fatalf("parse representative run options: %v", err)
	}
	if opts.samplePerCategory != 1 || opts.repeat != 3 {
		t.Fatalf("options = %+v", opts)
	}
}

func TestCompareCommandRequiresOnlyReportAndBaseline(t *testing.T) {
	if _, err := parseOptions([]string{"compare", "--baseline", "approved.yaml"}); err == nil {
		t.Fatal("compare accepted a missing report")
	}
	opts, err := parseOptions([]string{"compare", "--baseline", "approved.yaml", "--report", "report.json"})
	if err != nil {
		t.Fatalf("parse compare options: %v", err)
	}
	if opts.baselinePath != "approved.yaml" || opts.reportPath != "report.json" {
		t.Fatalf("options = %+v", opts)
	}
}

func TestValidateDatasetDirectoryReportsAllCases(t *testing.T) {
	var output bytes.Buffer
	if err := run(context.Background(), []string{"validate", "--cases", "../../manifest/eval/cases"}, &output); err != nil {
		t.Fatalf("validate Dataset: %v", err)
	}
	if !bytes.Contains(output.Bytes(), []byte(`"cases":40`)) || !bytes.Contains(output.Bytes(), []byte(`"dataset_schema":"sentinelops/eval-dataset/v1"`)) {
		t.Fatalf("validation report = %s", output.String())
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
