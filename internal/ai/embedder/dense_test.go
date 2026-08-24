package embedder

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/embedding"
)

type fakeEmbedder struct {
	vectors [][]float64
}

func (f fakeEmbedder) EmbedStrings(context.Context, []string, ...embedding.Option) ([][]float64, error) {
	return f.vectors, nil
}

func TestDimensionCheckingEmbedderRequires2048Dimensions(t *testing.T) {
	good := &dimensionCheckingEmbedder{inner: fakeEmbedder{vectors: [][]float64{make([]float64, 2048)}}, dimension: 2048}
	if _, err := good.EmbedStrings(context.Background(), []string{"ok"}); err != nil {
		t.Fatalf("valid vector rejected: %v", err)
	}

	bad := &dimensionCheckingEmbedder{inner: fakeEmbedder{vectors: [][]float64{make([]float64, 1024)}}, dimension: 2048}
	if _, err := bad.EmbedStrings(context.Background(), []string{"bad"}); err == nil || !strings.Contains(err.Error(), "2048") {
		t.Fatalf("dimension error = %v, want 2048", err)
	}
}
