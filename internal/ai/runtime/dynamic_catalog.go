package runtime

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
)

type dynamicToolCatalogContextKey struct{}

// DynamicToolCatalogEntry 是本次 MCP Agent 运行中已通过官方 mapper 的只读 Tool 身份。
// 它只保存非敏感名称、revision、schema hash 和聚合 catalog hash。
type DynamicToolCatalogEntry struct {
	Name       string
	Revision   string
	SchemaHash string
	ReadOnly   bool
}

// DynamicToolCatalog 是一次 Agent Run 的动态 Tool Catalog，不作为进程级真值。
type DynamicToolCatalog struct {
	Hash    string
	Entries []DynamicToolCatalogEntry
}

// NewDynamicToolCatalog 校验并复制动态 MCP Tool Catalog。
func NewDynamicToolCatalog(hash string, entries []DynamicToolCatalogEntry) (DynamicToolCatalog, error) {
	if len(entries) > 0 && !validDynamicHash(hash) {
		return DynamicToolCatalog{}, fmt.Errorf("dynamic MCP catalog hash must be a 64-character SHA-256")
	}
	seen := make(map[string]struct{}, len(entries))
	cloned := make([]DynamicToolCatalogEntry, len(entries))
	for index, entry := range entries {
		if strings.TrimSpace(entry.Name) == "" || strings.TrimSpace(entry.Revision) == "" || !validDynamicHash(entry.SchemaHash) {
			return DynamicToolCatalog{}, fmt.Errorf("dynamic MCP catalog entry %d is incomplete", index)
		}
		if _, ok := seen[entry.Name]; ok {
			return DynamicToolCatalog{}, fmt.Errorf("duplicate dynamic MCP Tool %q", entry.Name)
		}
		if !entry.ReadOnly {
			return DynamicToolCatalog{}, fmt.Errorf("dynamic MCP Tool %q is not read-only", entry.Name)
		}
		seen[entry.Name] = struct{}{}
		cloned[index] = entry
	}
	return DynamicToolCatalog{Hash: hash, Entries: cloned}, nil
}

// WithDynamicToolCatalog 将本次 MCP Agent 的动态 Catalog 放入调用链 context。
func WithDynamicToolCatalog(ctx context.Context, catalog DynamicToolCatalog) context.Context {
	if ctx == nil {
		return nil
	}
	return context.WithValue(ctx, dynamicToolCatalogContextKey{}, cloneDynamicToolCatalog(catalog))
}

// DynamicToolCatalogFromContext 读取当前调用链的动态 Catalog 副本。
func DynamicToolCatalogFromContext(ctx context.Context) (DynamicToolCatalog, bool) {
	if ctx == nil {
		return DynamicToolCatalog{}, false
	}
	catalog, ok := ctx.Value(dynamicToolCatalogContextKey{}).(DynamicToolCatalog)
	if !ok {
		return DynamicToolCatalog{}, false
	}
	return cloneDynamicToolCatalog(catalog), true
}

// Lookup 查找动态 Tool 的只读身份。
func (c DynamicToolCatalog) Lookup(name string) (DynamicToolCatalogEntry, bool) {
	for _, entry := range c.Entries {
		if entry.Name == name {
			return entry, true
		}
	}
	return DynamicToolCatalogEntry{}, false
}

func cloneDynamicToolCatalog(catalog DynamicToolCatalog) DynamicToolCatalog {
	catalog.Entries = append([]DynamicToolCatalogEntry(nil), catalog.Entries...)
	return catalog
}

func validDynamicHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
