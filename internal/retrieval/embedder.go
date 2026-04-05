package retrieval

import (
	"context"
	"fmt"

	"github.com/rillanai/rillan/internal/index"
	"github.com/rillanai/rillan/internal/ollama"
)

// QueryEmbedder produces a vector embedding for a search query.
type QueryEmbedder interface {
	EmbedQuery(ctx context.Context, query string) ([]float32, error)
}

// PlaceholderEmbedder uses the deterministic SHA256-derived 8-dim embedding.
type PlaceholderEmbedder struct{}

func (PlaceholderEmbedder) EmbedQuery(_ context.Context, query string) ([]float32, error) {
	return index.PlaceholderEmbedding(query), nil
}

// OllamaEmbedder calls the Ollama embed endpoint for real semantic vectors.
type OllamaEmbedder struct {
	client *ollama.Client
	model  string
}

// NewOllamaEmbedder creates an embedder backed by an Ollama instance.
func NewOllamaEmbedder(client *ollama.Client, model string) *OllamaEmbedder {
	return &OllamaEmbedder{client: client, model: model}
}

func (e *OllamaEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	return e.client.Embed(ctx, e.model, query)
}

// FallbackEmbedder preserves retrieval behavior when the primary local-model
// embedder is unavailable by falling back to deterministic placeholder vectors.
type FallbackEmbedder struct {
	primary  QueryEmbedder
	fallback QueryEmbedder
}

// NewFallbackEmbedder creates an embedder that retries with a secondary
// implementation when the primary embedder fails.
func NewFallbackEmbedder(primary QueryEmbedder, fallback QueryEmbedder) *FallbackEmbedder {
	return &FallbackEmbedder{primary: primary, fallback: fallback}
}

func (e *FallbackEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	if e.primary == nil {
		return nil, fmt.Errorf("primary embedder is nil")
	}

	embedding, err := e.primary.EmbedQuery(ctx, query)
	if err == nil {
		return embedding, nil
	}

	if e.fallback == nil {
		return nil, err
	}

	return e.fallback.EmbedQuery(ctx, query)
}
