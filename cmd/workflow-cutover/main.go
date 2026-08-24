// workflow-cutover 提供 legacy Run cutover 的一次性、默认只读管理入口。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/config"
	"SentinelOps/internal/dao/mysql"
)

const cutoverConfirmation = "legacy-cutover-not-resumable"

type stringList []string

func (values *stringList) String() string { return strings.Join(*values, ",") }

func (values *stringList) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("run id must not be empty")
	}
	*values = append(*values, value)
	return nil
}

type options struct {
	dsnRef  config.SecretRef
	execute bool
	confirm string
	limit   int
	runIDs  []string
}

type preview struct {
	Mode       string                      `json:"mode"`
	Stats      workflow.LegacyCutoverStats `json:"stats"`
	Candidates []string                    `json:"candidates"`
}

func parseOptions(args []string) (options, error) {
	flags := flag.NewFlagSet("workflow-cutover", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var opts options
	var runIDs stringList
	var dsnRef string
	flags.StringVar(&dsnRef, "dsn-ref", "", "SecretRef containing the MySQL DSN")
	flags.BoolVar(&opts.execute, "execute", false, "terminalize the explicit legacy Run allowlist")
	flags.StringVar(&opts.confirm, "confirm", "", "required cutover confirmation phrase")
	flags.IntVar(&opts.limit, "limit", 100, "maximum preview candidates")
	flags.Var(&runIDs, "run-id", "legacy Run ID to terminalize; repeat for each approved Run")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	if flags.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected positional arguments")
	}
	opts.dsnRef = config.SecretRef(strings.TrimSpace(dsnRef))
	if err := opts.dsnRef.Validate(); err != nil {
		return options{}, fmt.Errorf("invalid --dsn-ref: %w", err)
	}
	opts.runIDs = append([]string(nil), runIDs...)
	if opts.limit <= 0 || opts.limit > 1000 {
		return options{}, fmt.Errorf("--limit must be between 1 and 1000")
	}
	if opts.execute {
		if opts.confirm != cutoverConfirmation {
			return options{}, fmt.Errorf("--execute requires --confirm=%s", cutoverConfirmation)
		}
		if len(opts.runIDs) == 0 {
			return options{}, fmt.Errorf("--execute requires at least one explicit --run-id")
		}
	} else if len(opts.runIDs) != 0 || opts.confirm != "" {
		return options{}, fmt.Errorf("--run-id and --confirm are only valid with --execute")
	}
	return opts, nil
}

func run(ctx context.Context, args []string, output io.Writer) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	return config.UseSecret(ctx, opts.dsnRef, func(dsn []byte) error {
		if err := mysql.InitWithDSN(ctx, dsn); err != nil {
			return err
		}
		db, err := mysql.DB(ctx)
		if err != nil {
			return err
		}
		sqlDB, err := db.DB()
		if err != nil {
			return fmt.Errorf("read MySQL connection: %w", err)
		}
		defer sqlDB.Close()

		store := workflow.NewGORMStore(db)
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		if !opts.execute {
			stats, err := store.LegacyCutoverStats(ctx)
			if err != nil {
				return err
			}
			candidates, err := store.ListLegacyNonTerminalRunIDs(ctx, opts.limit)
			if err != nil {
				return err
			}
			return encoder.Encode(preview{Mode: "preview", Stats: stats, Candidates: candidates})
		}

		result, err := store.TerminalizeLegacyRuns(ctx, opts.runIDs)
		if err != nil {
			return err
		}
		return encoder.Encode(result)
	})
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
