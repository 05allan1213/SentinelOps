package mysql

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestMetricReuseWorkflowTraceLookupUsesScopedLatestAttempt(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "trace_dao.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "GetRunByWorkflowRunID" || function.Body == nil {
			continue
		}
		callsScopedTrace := false
		callsEvidenceScope := false
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			identifier, ok := call.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			callsScopedTrace = callsScopedTrace || identifier.Name == "scopedTraceRuns"
			callsEvidenceScope = callsEvidenceScope || identifier.Name == "scopedEvidenceTraceRuns"
			return true
		})
		if !callsScopedTrace || callsEvidenceScope {
			t.Fatalf("workflow Trace lookup must read the scoped latest Attempt before checking evidence quality")
		}
		return
	}
	t.Fatal("GetRunByWorkflowRunID not found")
}
