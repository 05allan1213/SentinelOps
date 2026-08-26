package rageval

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestMetricReuseKeepsDashboardOnExistingTraceEvidence(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "service.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "GetDashboard" || function.Body == nil {
			continue
		}
		evidenceScopeCalls := 0
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			identifier, ok := call.Fun.(*ast.Ident)
			if ok && identifier.Name == "scopedEvidenceTraceRuns" {
				evidenceScopeCalls++
			}
			return true
		})
		if evidenceScopeCalls != 2 {
			t.Fatalf("GetDashboard evidence scopes = %d, want existing Run and Retriever scopes", evidenceScopeCalls)
		}
		return
	}
	t.Fatal("GetDashboard not found")
}
