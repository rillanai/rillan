package retrieval

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rillanai/rillan/internal/ollama"
)

func TestPlaceholderEmbedderReturnsEightDimensions(t *testing.T) {
	t.Parallel()

	embedder := PlaceholderEmbedder{}
	result, err := embedder.EmbedQuery(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 8 {
		t.Fatalf("expected 8 dimensions, got %d", len(result))
	}
}

func TestOllamaEmbedderCallsOllamaAPI(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"embeddings": [][]float64{{0.5, 0.6, 0.7}},
		})
	}))
	defer server.Close()

	client := ollama.New(server.URL, server.Client())
	embedder := NewOllamaEmbedder(client, "nomic-embed-text")

	result, err := embedder.EmbedQuery(context.Background(), "test query")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 dimensions, got %d", len(result))
	}
}

func TestFallbackEmbedderFallsBackWhenPrimaryFails(t *testing.T) {
	t.Parallel()

	embedder := NewFallbackEmbedder(
		failingQueryEmbedder{err: errors.New("ollama unavailable")},
		PlaceholderEmbedder{},
	)

	result, err := embedder.EmbedQuery(context.Background(), "test query")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 8 {
		t.Fatalf("expected placeholder embedding dimensions, got %d", len(result))
	}
}

type failingQueryEmbedder struct {
	err error
}

func (f failingQueryEmbedder) EmbedQuery(context.Context, string) ([]float32, error) {
	return nil, f.err
}
