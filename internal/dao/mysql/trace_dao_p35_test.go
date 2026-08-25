package mysql

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestTraceIncompleteAggregateContract(t *testing.T) {
	if EvidenceTraceQualityPredicate != "COALESCE(CASE WHEN JSON_VALID(agent_trace_runs.tags) THEN JSON_UNQUOTE(JSON_EXTRACT(agent_trace_runs.tags, '$.trace_quality')) ELSE NULL END, 'unknown') <> 'incomplete'" {
		t.Fatalf("evidence predicate = %q", EvidenceTraceQualityPredicate)
	}

	file, err := parser.ParseFile(token.NewFileSet(), "trace_dao.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	usesEvidenceScope := map[string]bool{}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			identifier, ok := call.Fun.(*ast.Ident)
			if ok && identifier.Name == "scopedEvidenceTraceRuns" {
				usesEvidenceScope[function.Name.Name] = true
			}
			return true
		})
	}

	for _, name := range []string{
		"GetStatsAgg", "GetSuccessDurations", "GetCostAgg", "GetDailyCostTrend",
		"GetModelCostBreakdown", "GetIntentCostBreakdown", "GetHourlyTokenTrend",
	} {
		if !usesEvidenceScope[name] {
			t.Errorf("%s does not exclude incomplete Trace evidence", name)
		}
	}
	for _, name := range []string{"ListRuns", "GetRunByTraceID", "ListNodesByTraceID", "ListRunsBySessionID"} {
		if usesEvidenceScope[name] {
			t.Errorf("%s hides incomplete diagnostic Trace data", name)
		}
	}
}
