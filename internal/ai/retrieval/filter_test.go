package retrieval

import (
	"context"
	"errors"
	"testing"

	"SentinelOps/internal/ai/budgetctx"
	"SentinelOps/internal/ai/evidence"

	"github.com/cloudwego/eino/schema"
)

type recordingRAGProvider struct {
	docs, chars int64
	identity    string
	settled     bool
}
type recordingRAGReservation struct{ provider *recordingRAGProvider }

func (p *recordingRAGProvider) ReserveRAG(_ context.Context, identity string, docs, chars int64) (budgetctx.RAGReservation, error) {
	p.identity, p.docs, p.chars = identity, docs, chars
	return &recordingRAGReservation{provider: p}, nil
}
func (r *recordingRAGReservation) Settle(_ context.Context, docs, chars int64, _ bool) error {
	r.provider.docs, r.provider.chars, r.provider.settled = docs, chars, true
	return nil
}

func TestFilterDisabledDocsRequiresScope(t *testing.T) {
	docs := []*schema.Document{{ID: "chunk-1", MetaData: map[string]any{"doc_id": "doc-1"}}}
	_, err := FilterDisabledDocsStrict(context.Background(), docs)
	if !errors.Is(err, ErrEvidenceUnavailable) {
		t.Fatalf("missing Scope error = %v", err)
	}
	if got := FilterDisabledDocs(context.Background(), docs); got != nil {
		t.Fatalf("fail-closed filter returned %#v", got)
	}
}

func TestFilterDocumentsEmptyAndScopePredicate(t *testing.T) {
	scope := evidence.Scope{UserID: "u1", Role: "viewer", AccessScope: "user:u1"}
	got, err := FilterDocuments(context.Background(), nil, scope)
	if err != nil || got != nil {
		t.Fatalf("empty docs = %#v, %v", got, err)
	}
	if scope.Allows("user:u2") {
		t.Fatal("cross-user scope must be denied")
	}
}

func TestRAGReservationUsesSharedBudgetDimensions(t *testing.T) {
	provider := &recordingRAGProvider{}
	ctx := budgetctx.WithProvider(context.Background(), provider)
	reservation, err := reserveRAG(ctx, Config{Partition: "documents", TopK: 3}, "query")
	if err != nil {
		t.Fatal(err)
	}
	if provider.docs != 3 || provider.chars != 24000 || provider.identity == "" {
		t.Fatalf("reserve estimate = docs %d chars %d identity %q", provider.docs, provider.chars, provider.identity)
	}
	if err := reservation.settle(ctx, []*schema.Document{{Content: "abcd"}}, true); err != nil {
		t.Fatal(err)
	}
	if !provider.settled || provider.docs != 1 || provider.chars != 4 {
		t.Fatalf("settle actual = docs %d chars %d settled=%v", provider.docs, provider.chars, provider.settled)
	}
}
