package cache

import (
	"testing"

	"SentinelOps/internal/ai/evidence"
)

func TestSemanticCacheScopeNamespaceIsolation(t *testing.T) {
	c := New(nil, "rag:cache", 24, .85)
	base := evidence.Scope{UserID: "u1", Role: "viewer", KnowledgeBaseID: "base-1", AccessScope: "user:u1", IndexedVersion: 1, PolicyHash: "policy-1"}
	for name, changed := range map[string]evidence.Scope{
		"user":    {UserID: "u2", Role: base.Role, KnowledgeBaseID: base.KnowledgeBaseID, AccessScope: base.AccessScope, IndexedVersion: base.IndexedVersion, PolicyHash: base.PolicyHash},
		"role":    {UserID: base.UserID, Role: "operator", KnowledgeBaseID: base.KnowledgeBaseID, AccessScope: base.AccessScope, IndexedVersion: base.IndexedVersion, PolicyHash: base.PolicyHash},
		"base":    {UserID: base.UserID, Role: base.Role, KnowledgeBaseID: "base-2", AccessScope: base.AccessScope, IndexedVersion: base.IndexedVersion, PolicyHash: base.PolicyHash},
		"version": {UserID: base.UserID, Role: base.Role, KnowledgeBaseID: base.KnowledgeBaseID, AccessScope: base.AccessScope, IndexedVersion: 2, PolicyHash: base.PolicyHash},
		"policy":  {UserID: base.UserID, Role: base.Role, KnowledgeBaseID: base.KnowledgeBaseID, AccessScope: base.AccessScope, IndexedVersion: base.IndexedVersion, PolicyHash: "policy-2"},
	} {
		if got := c.ScopedKeyPrefix(base); got == c.ScopedKeyPrefix(changed) {
			t.Fatalf("%s crossed cache namespace", name)
		}
	}
}
