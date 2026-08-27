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
	command                  string
	casesPath                string
	baseURL                  string
	baselinePath             string
	reportPath               string
	dsnRef                   config.SecretRef
	authorizationRef         config.SecretRef
	viewerAuthorizationRef   config.SecretRef
	operatorAuthorizationRef config.SecretRef
	approverAuthorizationRef config.SecretRef
	adminAuthorizationRef    config.SecretRef
	scenarioProcessesPath    string
	timeout                  time.Duration
	interval                 time.Duration
	samplePerCategory        int
	repeat                   int
}

type report = aieval.EvaluationReport

func parseOptions(args []string) (options, error) {
	if len(args) == 0 {
		return options{}, fmt.Errorf("command is required: validate, run or compare")
	}
	opts := options{command: strings.TrimSpace(args[0]), timeout: 15 * time.Minute, interval: 250 * time.Millisecond, repeat: 1}
	if opts.command != "validate" && opts.command != "run" && opts.command != "compare" {
		return options{}, fmt.Errorf("unsupported command %q", opts.command)
	}
	flags := flag.NewFlagSet("agenteval "+opts.command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&opts.casesPath, "cases", "", "YAML/JSON Eval Case file or Dataset directory")
	flags.StringVar(&opts.baseURL, "base-url", "", "production API base URL")
	flags.StringVar(&opts.baselinePath, "baseline", "", "approved baseline YAML file")
	flags.StringVar(&opts.reportPath, "report", "", "Eval report JSON file")
	var dsnRef, authorizationRef string
	var viewerAuthorizationRef, operatorAuthorizationRef, approverAuthorizationRef, adminAuthorizationRef string
	flags.StringVar(&dsnRef, "dsn-ref", "", "SecretRef containing the MySQL DSN")
	flags.StringVar(&authorizationRef, "authorization-ref", "", "SecretRef containing a bearer token")
	flags.StringVar(&viewerAuthorizationRef, "viewer-authorization-ref", "", "SecretRef containing the viewer bearer token")
	flags.StringVar(&operatorAuthorizationRef, "operator-authorization-ref", "", "SecretRef containing the operator bearer token")
	flags.StringVar(&approverAuthorizationRef, "approver-authorization-ref", "", "SecretRef containing the approver bearer token")
	flags.StringVar(&adminAuthorizationRef, "admin-authorization-ref", "", "SecretRef containing the admin bearer token")
	flags.StringVar(&opts.scenarioProcessesPath, "scenario-processes", "", "strict JSON manifest of local P43 scenario processes")
	flags.DurationVar(&opts.timeout, "timeout", opts.timeout, "maximum duration for all cases")
	flags.DurationVar(&opts.interval, "poll-interval", opts.interval, "MySQL truth polling interval")
	flags.IntVar(&opts.samplePerCategory, "sample-per-category", 0, "number of representative Dataset cases per category")
	flags.IntVar(&opts.repeat, "repeat", opts.repeat, "number of times to execute each selected case")
	if err := flags.Parse(args[1:]); err != nil {
		return options{}, err
	}
	if flags.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected positional arguments")
	}
	opts.dsnRef = config.SecretRef(strings.TrimSpace(dsnRef))
	opts.authorizationRef = config.SecretRef(strings.TrimSpace(authorizationRef))
	opts.viewerAuthorizationRef = config.SecretRef(strings.TrimSpace(viewerAuthorizationRef))
	opts.operatorAuthorizationRef = config.SecretRef(strings.TrimSpace(operatorAuthorizationRef))
	opts.approverAuthorizationRef = config.SecretRef(strings.TrimSpace(approverAuthorizationRef))
	opts.adminAuthorizationRef = config.SecretRef(strings.TrimSpace(adminAuthorizationRef))
	opts.scenarioProcessesPath = strings.TrimSpace(opts.scenarioProcessesPath)
	if opts.timeout <= 0 || opts.interval <= 0 || opts.repeat <= 0 || opts.samplePerCategory < 0 {
		return options{}, fmt.Errorf("timeout, poll interval and repeat must be positive; sample-per-category must not be negative")
	}
	switch opts.command {
	case "validate":
		if strings.TrimSpace(opts.casesPath) == "" {
			return options{}, fmt.Errorf("--cases is required")
		}
		if opts.baseURL != "" || dsnRef != "" || authorizationRef != "" || hasIdentityAuthorizationRefs(opts) || opts.scenarioProcessesPath != "" || opts.baselinePath != "" || opts.reportPath != "" || opts.samplePerCategory != 0 || opts.repeat != 1 {
			return options{}, fmt.Errorf("validate only accepts --cases")
		}
		return opts, nil
	case "run":
		if strings.TrimSpace(opts.casesPath) == "" {
			return options{}, fmt.Errorf("--cases is required")
		}
		if strings.TrimSpace(opts.baseURL) == "" {
			return options{}, fmt.Errorf("--base-url is required for run")
		}
		if opts.baselinePath != "" || opts.reportPath != "" {
			return options{}, fmt.Errorf("--baseline and --report are only valid with compare")
		}
		if err := opts.dsnRef.Validate(); err != nil {
			return options{}, fmt.Errorf("invalid --dsn-ref: %w", err)
		}
		if authorizationRef != "" {
			if err := opts.authorizationRef.Validate(); err != nil {
				return options{}, fmt.Errorf("invalid --authorization-ref: %w", err)
			}
		}
		if authorizationRef != "" && hasIdentityAuthorizationRefs(opts) {
			return options{}, fmt.Errorf("--authorization-ref cannot be combined with identity-specific authorization refs")
		}
		for identity, ref := range identityAuthorizationRefs(opts) {
			if ref != "" {
				if err := ref.Validate(); err != nil {
					return options{}, fmt.Errorf("invalid --%s-authorization-ref: %w", identity, err)
				}
			}
		}
		return opts, nil
	case "compare":
		if strings.TrimSpace(opts.baselinePath) == "" || strings.TrimSpace(opts.reportPath) == "" {
			return options{}, fmt.Errorf("--baseline and --report are required for compare")
		}
		if opts.casesPath != "" || opts.baseURL != "" || dsnRef != "" || authorizationRef != "" || hasIdentityAuthorizationRefs(opts) || opts.scenarioProcessesPath != "" || opts.samplePerCategory != 0 || opts.repeat != 1 {
			return options{}, fmt.Errorf("compare only accepts --baseline and --report")
		}
		return opts, nil
	default:
		return options{}, fmt.Errorf("unsupported command %q", opts.command)
	}
}

func run(ctx context.Context, args []string, output io.Writer) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	if opts.command == "compare" {
		return compareReport(opts, output)
	}
	dataset, datasetCases, cases, err := loadCaseInput(opts.casesPath, opts.samplePerCategory)
	if err != nil {
		return err
	}
	if opts.command == "validate" {
		result := report{Schema: aieval.ReportSchema, EvalRunID: "validation", Command: opts.command, Repeat: 1, Cases: len(cases), Passed: len(cases)}
		if dataset != nil {
			result.DatasetSchema, result.DatasetVersion, result.Repeat = dataset.Schema, dataset.Version, dataset.Repeat
			result.Snapshots, err = aieval.SnapshotIdentitiesFromCases(datasetCases)
			if err != nil {
				return err
			}
		}
		result.Summary = validationSummary(len(cases))
		return json.NewEncoder(output).Encode(result)
	}
	deadline, cancel := context.WithTimeout(ctx, opts.timeout)
	defer cancel()
	return withRuntimeAdapter(deadline, opts, cases, func(adapter *aieval.HTTPRuntimeAdapter) error {
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
			result := report{Schema: aieval.ReportSchema, EvalRunID: evalRunID, Command: opts.command, Repeat: opts.repeat, Cases: len(cases) * opts.repeat, Results: make([]aieval.CaseResult, 0, len(cases)*opts.repeat)}
			if dataset != nil {
				result.DatasetSchema, result.DatasetVersion = dataset.Schema, dataset.Version
				result.Snapshots, err = aieval.SnapshotIdentitiesFromCases(datasetCases)
				if err != nil {
					return err
				}
			}
			for repeat := 0; repeat < opts.repeat; repeat++ {
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
			}
			result.Summary, err = aieval.SummarizeResults(cases, result.Results)
			if err != nil {
				return err
			}
			if err := json.NewEncoder(output).Encode(result); err != nil {
				return err
			}
			if err := aieval.ValidateReleaseThresholds(result.Summary); err != nil {
				return err
			}
			return nil
		})
	})
}

func compareReport(opts options, output io.Writer) error {
	reportData, err := os.ReadFile(opts.reportPath)
	if err != nil {
		return fmt.Errorf("read report: %w", err)
	}
	var evaluationReport report
	decoder := json.NewDecoder(strings.NewReader(string(reportData)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&evaluationReport); err != nil {
		return fmt.Errorf("decode report: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err != nil {
			return fmt.Errorf("decode report: %w", err)
		}
		return fmt.Errorf("decode report: multiple JSON documents are not supported")
	}
	baselineData, err := os.ReadFile(opts.baselinePath)
	if err != nil {
		return fmt.Errorf("read baseline: %w", err)
	}
	baseline, err := aieval.LoadBaseline(strings.NewReader(string(baselineData)))
	if err != nil {
		return err
	}
	comparison, err := aieval.CompareReport(evaluationReport, baseline)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(output).Encode(comparison); err != nil {
		return err
	}
	if !comparison.Passed {
		return fmt.Errorf("baseline comparison failed")
	}
	return nil
}

func loadCaseInput(path string, samplePerCategory int) (*aieval.Dataset, []aieval.DatasetCase, []aieval.EvalCase, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("inspect cases: %w", err)
	}
	if info.IsDir() {
		dataset, err := aieval.LoadDatasetDirectory(path)
		if err != nil {
			return nil, nil, nil, err
		}
		datasetCases := dataset.Cases
		if samplePerCategory > 0 {
			datasetCases, err = aieval.SelectSamplePerCategory(dataset, samplePerCategory)
			if err != nil {
				return nil, nil, nil, err
			}
		}
		cases := make([]aieval.EvalCase, 0, len(datasetCases))
		for _, item := range datasetCases {
			cases = append(cases, item.ToEvalCase())
		}
		return &dataset, datasetCases, cases, nil
	}
	if samplePerCategory > 0 {
		return nil, nil, nil, fmt.Errorf("--sample-per-category requires a Dataset directory")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read cases: %w", err)
	}
	cases, err := evalCases(data)
	if err != nil {
		return nil, nil, nil, err
	}
	return nil, nil, cases, nil
}

func evalCases(data []byte) ([]aieval.EvalCase, error) {
	cases, err := aieval.LoadCases(strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("load cases: %w", err)
	}
	return cases, nil
}

func validationSummary(count int) aieval.MetricSummary {
	return aieval.MetricSummary{Cases: count, Passed: count, TaskSuccessRate: 1, ToolSelectionPassRate: 1, ToolCallSuccessRate: 1}
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

func withRuntimeAdapter(
	ctx context.Context,
	opts options,
	cases []aieval.EvalCase,
	use func(*aieval.HTTPRuntimeAdapter) error,
) error {
	if use == nil {
		return fmt.Errorf("runtime adapter consumer is required")
	}
	if casesRequireFaultController(cases) && opts.scenarioProcessesPath == "" {
		return fmt.Errorf("recovery and dependency scenarios require --scenario-processes")
	}
	configure := func(adapter *aieval.HTTPRuntimeAdapter) error {
		defer adapter.Close()
		if opts.scenarioProcessesPath != "" {
			file, err := os.Open(opts.scenarioProcessesPath)
			if err != nil {
				return fmt.Errorf("open local scenario process manifest: %w", err)
			}
			controller, loadErr := aieval.LoadLocalProcessFaultController(file)
			closeErr := file.Close()
			if loadErr != nil {
				return loadErr
			}
			if closeErr != nil {
				return fmt.Errorf("close local scenario process manifest: %w", closeErr)
			}
			adapter.Faults = controller
		}
		return use(adapter)
	}
	if hasIdentityAuthorizationRefs(opts) {
		refs := identityAuthorizationRefs(opts)
		if err := validateRequiredIdentityRefs(cases, refs); err != nil {
			return err
		}
		return withIdentityAuthorization(ctx, refs, func(headers map[aieval.ExecutionIdentity]http.Header) error {
			adapter, err := aieval.NewHTTPRuntimeAdapterWithIdentities(opts.baseURL, http.DefaultClient, headers)
			if err != nil {
				return err
			}
			return configure(adapter)
		})
	}
	if casesRequireSeparateDecisionIdentity(cases) {
		return fmt.Errorf("approval scenarios require identity-specific authorization refs")
	}
	return withAuthorization(ctx, opts.authorizationRef, func(header http.Header) error {
		adapter, err := aieval.NewHTTPRuntimeAdapter(opts.baseURL, http.DefaultClient, header)
		if err != nil {
			return err
		}
		return configure(adapter)
	})
}

func identityAuthorizationRefs(opts options) map[aieval.ExecutionIdentity]config.SecretRef {
	return map[aieval.ExecutionIdentity]config.SecretRef{
		aieval.ExecutionIdentityViewer: opts.viewerAuthorizationRef, aieval.ExecutionIdentityOperator: opts.operatorAuthorizationRef,
		aieval.ExecutionIdentityApprover: opts.approverAuthorizationRef, aieval.ExecutionIdentityAdmin: opts.adminAuthorizationRef,
	}
}

func hasIdentityAuthorizationRefs(opts options) bool {
	for _, ref := range identityAuthorizationRefs(opts) {
		if ref != "" {
			return true
		}
	}
	return false
}

func validateRequiredIdentityRefs(cases []aieval.EvalCase, refs map[aieval.ExecutionIdentity]config.SecretRef) error {
	for _, item := range cases {
		if item.ExecutionIdentity == "" {
			return fmt.Errorf("identity-specific authorization requires every Case to declare execution_identity")
		}
		if refs[item.ExecutionIdentity] == "" {
			return fmt.Errorf("authorization ref is missing for required eval identity %q", item.ExecutionIdentity)
		}
		if item.Scenario.DecisionIdentity != "" && refs[item.Scenario.DecisionIdentity] == "" {
			return fmt.Errorf("authorization ref is missing for required eval identity %q", item.Scenario.DecisionIdentity)
		}
	}
	return nil
}

func casesRequireSeparateDecisionIdentity(cases []aieval.EvalCase) bool {
	for _, item := range cases {
		if item.Scenario.DecisionIdentity != "" {
			return true
		}
	}
	return false
}

func casesRequireFaultController(cases []aieval.EvalCase) bool {
	for _, item := range cases {
		switch item.Scenario.Kind {
		case aieval.ScenarioCheckpointResume, aieval.ScenarioPreCheckpointReplay, aieval.ScenarioDependencyParked:
			return true
		}
	}
	return false
}

func withIdentityAuthorization(
	ctx context.Context,
	refs map[aieval.ExecutionIdentity]config.SecretRef,
	use func(map[aieval.ExecutionIdentity]http.Header) error,
) error {
	identities := []aieval.ExecutionIdentity{
		aieval.ExecutionIdentityViewer, aieval.ExecutionIdentityOperator, aieval.ExecutionIdentityApprover, aieval.ExecutionIdentityAdmin,
	}
	headers := make(map[aieval.ExecutionIdentity]http.Header, len(refs))
	var resolve func(int) error
	resolve = func(index int) error {
		if index == len(identities) {
			return use(headers)
		}
		identity := identities[index]
		ref := refs[identity]
		if ref == "" {
			return resolve(index + 1)
		}
		return config.UseSecret(ctx, ref, func(value []byte) error {
			header := make(http.Header)
			header.Set("Authorization", "Bearer "+string(value))
			headers[identity] = header
			defer func() {
				header.Del("Authorization")
				delete(headers, identity)
			}()
			return resolve(index + 1)
		})
	}
	return resolve(0)
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
