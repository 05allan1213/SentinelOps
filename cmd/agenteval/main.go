// agenteval 通过生产 API/Worker 和 MySQL 真值执行最薄 Eval Case 调度。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	aieval "SentinelOps/internal/ai/eval"
	"SentinelOps/internal/config"
	"SentinelOps/internal/dao/mysql"

	"github.com/google/uuid"
)

type options struct {
	command          string
	casesPath        string
	baseURL          string
	dsnRef           config.SecretRef
	authorizationRef config.SecretRef
	timeout          time.Duration
	interval         time.Duration
}

type report struct {
	EvalRunID string              `json:"eval_run_id"`
	Command   string              `json:"command"`
	Cases     int                 `json:"cases"`
	Passed    int                 `json:"passed"`
	Results   []aieval.CaseResult `json:"results,omitempty"`
}

func parseOptions(args []string) (options, error) {
	if len(args) == 0 {
		return options{}, fmt.Errorf("command is required: validate or run")
	}
	opts := options{command: strings.TrimSpace(args[0]), timeout: 15 * time.Minute, interval: 250 * time.Millisecond}
	if opts.command != "validate" && opts.command != "run" {
		return options{}, fmt.Errorf("unsupported command %q", opts.command)
	}
	flags := flag.NewFlagSet("agenteval "+opts.command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&opts.casesPath, "cases", "", "YAML/JSON Eval Case file")
	flags.StringVar(&opts.baseURL, "base-url", "", "production API base URL")
	var dsnRef, authorizationRef string
	flags.StringVar(&dsnRef, "dsn-ref", "", "SecretRef containing the MySQL DSN")
	flags.StringVar(&authorizationRef, "authorization-ref", "", "SecretRef containing a bearer token")
	flags.DurationVar(&opts.timeout, "timeout", opts.timeout, "maximum duration for all cases")
	flags.DurationVar(&opts.interval, "poll-interval", opts.interval, "MySQL truth polling interval")
	if err := flags.Parse(args[1:]); err != nil {
		return options{}, err
	}
	if flags.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected positional arguments")
	}
	if strings.TrimSpace(opts.casesPath) == "" {
		return options{}, fmt.Errorf("--cases is required")
	}
	opts.dsnRef = config.SecretRef(strings.TrimSpace(dsnRef))
	opts.authorizationRef = config.SecretRef(strings.TrimSpace(authorizationRef))
	if opts.timeout <= 0 || opts.interval <= 0 {
		return options{}, fmt.Errorf("--timeout and --poll-interval must be positive")
	}
	if opts.command == "validate" {
		if opts.baseURL != "" || dsnRef != "" || authorizationRef != "" {
			return options{}, fmt.Errorf("--base-url and SecretRefs are only valid with run")
		}
		return opts, nil
	}
	if strings.TrimSpace(opts.baseURL) == "" {
		return options{}, fmt.Errorf("--base-url is required for run")
	}
	if err := opts.dsnRef.Validate(); err != nil {
		return options{}, fmt.Errorf("invalid --dsn-ref: %w", err)
	}
	if authorizationRef != "" {
		if err := opts.authorizationRef.Validate(); err != nil {
			return options{}, fmt.Errorf("invalid --authorization-ref: %w", err)
		}
	}
	return opts, nil
}

func run(ctx context.Context, args []string, output io.Writer) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(opts.casesPath)
	if err != nil {
		return fmt.Errorf("read cases: %w", err)
	}
	cases, err := evalCases(data)
	if err != nil {
		return err
	}
	if opts.command == "validate" {
		return json.NewEncoder(output).Encode(report{EvalRunID: "validation", Command: opts.command, Cases: len(cases), Passed: len(cases)})
	}
	deadline, cancel := context.WithTimeout(ctx, opts.timeout)
	defer cancel()
	return withAuthorization(deadline, opts.authorizationRef, func(header http.Header) error {
		adapter, err := aieval.NewHTTPRuntimeAdapter(opts.baseURL, http.DefaultClient, header)
		if err != nil {
			return err
		}
		defer adapter.Close()
		return config.UseSecret(deadline, opts.dsnRef, func(dsn []byte) error {
			if err := mysql.InitWithDSN(deadline, dsn); err != nil {
				return err
			}
			db, err := mysql.DB(deadline)
			if err != nil {
				return err
			}
			truth := &aieval.MySQLTruthReader{DB: db, Interval: opts.interval}
			evalRunID := uuid.NewString()
			result := report{EvalRunID: evalRunID, Command: opts.command, Cases: len(cases), Results: make([]aieval.CaseResult, 0, len(cases))}
			for _, item := range cases {
				caseResult, evalErr := aieval.EvaluateCase(deadline, adapter, truth, item)
				if evalErr != nil {
					return evalErr
				}
				caseResult.EvalRunID = evalRunID
				result.Results = append(result.Results, caseResult)
				if caseResult.Passed {
					result.Passed++
				}
			}
			if err := json.NewEncoder(output).Encode(result); err != nil {
				return err
			}
			if result.Passed != result.Cases {
				return fmt.Errorf("eval failed: %d/%d cases passed", result.Passed, result.Cases)
			}
			return nil
		})
	})
}

func evalCases(data []byte) ([]aieval.EvalCase, error) {
	cases, err := aieval.LoadCases(strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("load cases: %w", err)
	}
	return cases, nil
}

func withAuthorization(ctx context.Context, ref config.SecretRef, use func(http.Header) error) error {
	if strings.TrimSpace(string(ref)) == "" {
		return use(make(http.Header))
	}
	return config.UseSecret(ctx, ref, func(value []byte) error {
		header := make(http.Header)
		header.Set("Authorization", "Bearer "+string(value))
		defer header.Del("Authorization")
		return use(header)
	})
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
