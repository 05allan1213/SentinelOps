package base

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"sync"
	"testing"

	einoretriever "github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
)

type retrievalGolden struct {
	Simple retrievalGoldenCase `json:"simple"`
	Split  retrievalGoldenCase `json:"split"`
}

type retrievalGoldenCase struct {
	Stages    []string `json:"stages"`
	Queries   []string `json:"queries"`
	Documents []string `json:"documents"`
}

type retrievalTrace struct {
	mu      sync.Mutex
	stages  []string
	queries []string
}

func (t *retrievalTrace) stage(value string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stages = append(t.stages, value)
}

func (t *retrievalTrace) query(value string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.queries = append(t.queries, value)
}

func (t *retrievalTrace) snapshot(docs []*schema.Document) retrievalGoldenCase {
	t.mu.Lock()
	defer t.mu.Unlock()
	queries := append([]string(nil), t.queries...)
	sort.Strings(queries)
	ids := make([]string, 0, len(docs))
	for _, doc := range docs {
		ids = append(ids, doc.ID)
	}
	return retrievalGoldenCase{Stages: append([]string(nil), t.stages...), Queries: queries, Documents: ids}
}

type characterizationRetriever struct {
	trace *retrievalTrace
}

func (r *characterizationRetriever) Retrieve(_ context.Context, query string, _ ...einoretriever.Option) ([]*schema.Document, error) {
	r.trace.query(query)
	return []*schema.Document{{ID: query + ":b"}, {ID: "shared"}, {ID: query + ":a"}}, nil
}

func TestSharedRetrievalMatchesLegacyGolden(t *testing.T) {
	ctx := context.Background()
	input := &UserMessage{Query: "CVE 修复", History: []*schema.Message{schema.UserMessage("上一个漏洞")}}

	simpleTrace := &retrievalTrace{}
	simpleDocs, err := retrieveDocuments(ctx, input, RetrievalOptions{RewriteEnabled: true}, characterizationDependencies(simpleTrace))
	if err != nil {
		t.Fatal(err)
	}
	splitTrace := &retrievalTrace{}
	splitDocs, err := retrieveDocuments(ctx, input, RetrievalOptions{RewriteEnabled: true, SplitEnabled: true}, characterizationDependencies(splitTrace))
	if err != nil {
		t.Fatal(err)
	}

	got := retrievalGolden{Simple: simpleTrace.snapshot(simpleDocs), Split: splitTrace.snapshot(splitDocs)}
	gotJSON, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, err := os.ReadFile("testdata/shared_retrieval.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(gotJSON)+"\n" != string(wantJSON) {
		t.Fatalf("shared retrieval drifted\n got: %s\nwant: %s", gotJSON, wantJSON)
	}
}

func TestSharedRetrievalPreservesDocumentMetadata(t *testing.T) {
	metadata := map[string]any{"source": "legacy", "score": 0.75}
	deps := characterizationDependencies(&retrievalTrace{})
	deps.retriever = func() einoretriever.Retriever {
		return retrieverFunc(func(context.Context, string, ...einoretriever.Option) ([]*schema.Document, error) {
			return []*schema.Document{{ID: "doc", Content: "evidence", MetaData: metadata}}, nil
		})
	}
	docs, err := retrieveDocuments(context.Background(), &UserMessage{Query: "raw"}, RetrievalOptions{}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Content != "evidence" || !reflect.DeepEqual(docs[0].MetaData, metadata) {
		t.Fatalf("documents = %#v", docs)
	}
}

type retrieverFunc func(context.Context, string, ...einoretriever.Option) ([]*schema.Document, error)

func (f retrieverFunc) Retrieve(ctx context.Context, query string, opts ...einoretriever.Option) ([]*schema.Document, error) {
	return f(ctx, query, opts...)
}

func characterizationDependencies(trace *retrievalTrace) retrievalDependencies {
	return retrievalDependencies{
		normalize: func(_ context.Context, query string) string {
			trace.stage("normalize:" + query)
			return "normalized:" + query
		},
		rewriteQuery: func(_ context.Context, query string, _ []*schema.Message) string {
			trace.stage("rewrite:" + query)
			return "rewritten:" + query
		},
		rewriteAndSplit: func(_ context.Context, query string, _ []*schema.Message) (string, []string) {
			trace.stage("rewrite_split:" + query)
			return "rewritten:" + query, []string{"part-b:" + query, "part-a:" + query}
		},
		splitQuestions: func(_ context.Context, query string) []string {
			trace.stage("split:" + query)
			return []string{"part-b:" + query, "part-a:" + query}
		},
		retriever: func() einoretriever.Retriever {
			return &characterizationRetriever{trace: trace}
		},
		rerank: func(_ context.Context, query string, docs []*schema.Document, topN int) []*schema.Document {
			trace.stage("rerank:" + query)
			sorted := append([]*schema.Document(nil), docs...)
			sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
			if topN < len(sorted) {
				sorted = sorted[:topN]
			}
			return sorted
		},
	}
}
