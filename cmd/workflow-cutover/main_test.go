package main

import "testing"

func TestCutoverCommandDefaultsToPreview(t *testing.T) {
	opts, err := parseOptions([]string{"--dsn-ref=env:CUTOVER_TEST_DSN"})
	if err != nil {
		t.Fatalf("parse preview options: %v", err)
	}
	if opts.execute || opts.limit != 100 || len(opts.runIDs) != 0 {
		t.Fatalf("preview options = %+v", opts)
	}
}

func TestCutoverCommandRequiresExplicitAllowlistAndConfirmation(t *testing.T) {
	for _, args := range [][]string{
		{"--dsn-ref=env:CUTOVER_TEST_DSN", "--execute"},
		{"--dsn-ref=env:CUTOVER_TEST_DSN", "--execute", "--confirm=wrong", "--run-id=legacy"},
		{"--dsn-ref=env:CUTOVER_TEST_DSN", "--run-id=legacy"},
	} {
		if _, err := parseOptions(args); err == nil {
			t.Fatalf("unsafe args accepted: %v", args)
		}
	}

	opts, err := parseOptions([]string{
		"--dsn-ref=env:CUTOVER_TEST_DSN",
		"--execute",
		"--confirm=" + cutoverConfirmation,
		"--run-id=legacy-a",
		"--run-id=legacy-b",
	})
	if err != nil {
		t.Fatalf("parse explicit cutover options: %v", err)
	}
	if !opts.execute || len(opts.runIDs) != 2 {
		t.Fatalf("execute options = %+v", opts)
	}
}
