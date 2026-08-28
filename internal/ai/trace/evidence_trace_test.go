package trace

import (
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestEvidenceTraceSummaryStoresIDsAndBoundedTextOnly(t *testing.T) {
	content := strings.Repeat("sensitive evidence body ", 100)
	summaries := buildEvidenceTraceSummaries([]*schema.Document{{ID: "chunk-31", Content: content, MetaData: map[string]any{"source_id": "chunk-31", "source_version": "v1", "access_scope": "public", "content_hash": strings.Repeat("e", 64), "score": .9}}})
	if len(summaries) != 1 || summaries[0].EvidenceID == "" || summaries[0].SourceID != "chunk-31" {
		t.Fatalf("summaries=%#v", summaries)
	}
	if len([]rune(summaries[0].Summary)) > 163 || summaries[0].Summary == content {
		t.Fatalf("full evidence body leaked into trace summary: len=%d", len([]rune(summaries[0].Summary)))
	}
}
