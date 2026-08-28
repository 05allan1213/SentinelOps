package runtime

import (
	"context"
	"strings"
	"testing"
)

func TestP33DynamicCatalogIsReadOnlyAndImmutable(t *testing.T) {
	hash := strings.Repeat("a", 64)
	schemaHash := strings.Repeat("b", 64)
	catalog, err := NewDynamicToolCatalog(hash, []DynamicToolCatalogEntry{{
		Name: "inventory__search", Revision: "mcp_tool_v1", SchemaHash: schemaHash, ReadOnly: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithDynamicToolCatalog(context.Background(), catalog)
	got, ok := DynamicToolCatalogFromContext(ctx)
	if !ok {
		t.Fatal("dynamic catalog missing from context")
	}
	got.Entries[0].Name = "forged"
	stored, _ := DynamicToolCatalogFromContext(ctx)
	if stored.Entries[0].Name != "inventory__search" {
		t.Fatalf("dynamic catalog leaked mutable state: %+v", stored)
	}
	if _, err := NewDynamicToolCatalog(hash, []DynamicToolCatalogEntry{{
		Name: "inventory__write", Revision: "mcp_tool_v1", SchemaHash: schemaHash,
	}}); err == nil {
		t.Fatal("non-read-only MCP Tool was accepted")
	}
}
