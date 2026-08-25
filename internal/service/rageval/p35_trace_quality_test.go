package rageval

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestTraceIncompleteDashboardContract(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "service.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "GetDashboard" || function.Body == nil {
			continue
		}
		calls := 0
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			identifier, ok := call.Fun.(*ast.Ident)
			if ok && identifier.Name == "scopedEvidenceTraceRuns" {
				calls++
			}
			return true
		})
		if calls != 2 {
			t.Fatalf("GetDashboard evidence scopes = %d, want 2 for runs and retriever nodes", calls)
		}
		return
	}
	t.Fatal("GetDashboard not found")
}
