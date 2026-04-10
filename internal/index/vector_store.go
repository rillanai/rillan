// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package index

import (
	"context"
	"fmt"
)

const (
	VectorStoreModeEmbedded = "embedded"
	VectorStoreModeOllama   = "ollama"
)

// VectorStore builds embedding records for a set of chunks.
type VectorStore interface {
	BuildRecords(ctx context.Context, chunks []ChunkRecord) ([]VectorRecord, error)
	Mode() string
}

// Embedder generates a vector embedding for a single text input.
type Embedder interface {
	Embed(ctx context.Context, model string, text string) ([]float32, error)
}

type EmbeddedVectorStore struct{}

func NewVectorStore(mode string) (VectorStore, error) {
	switch mode {
	case "", VectorStoreModeEmbedded:
		return EmbeddedVectorStore{}, nil
	default:
		return nil, fmt.Errorf("unsupported vector store mode %q", mode)
	}
}

func (EmbeddedVectorStore) Mode() string {
	return VectorStoreModeEmbedded
}

func (EmbeddedVectorStore) BuildRecords(ctx context.Context, chunks []ChunkRecord) ([]VectorRecord, error) {
	vectors := make([]VectorRecord, 0, len(chunks))
	for _, chunk := range chunks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		embedding := PlaceholderEmbedding(chunk.Content)
		vectors = append(vectors, VectorRecord{
			ChunkID:    chunk.ID,
			Dimensions: len(embedding),
			Embedding:  EncodeEmbedding(embedding),
		})
	}

	return vectors, nil
}

// OllamaVectorStore produces real embeddings via a remote model.
type OllamaVectorStore struct {
	embedder Embedder
	model    string
}

// NewOllamaVectorStore creates a vector store that calls an Embedder for each chunk.
func NewOllamaVectorStore(embedder Embedder, model string) *OllamaVectorStore {
	return &OllamaVectorStore{embedder: embedder, model: model}
}

func (o *OllamaVectorStore) Mode() string {
	return VectorStoreModeOllama
}

func (o *OllamaVectorStore) BuildRecords(ctx context.Context, chunks []ChunkRecord) ([]VectorRecord, error) {
	vectors := make([]VectorRecord, 0, len(chunks))
	for _, chunk := range chunks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		embedding, err := o.embedder.Embed(ctx, o.model, chunk.Content)
		if err != nil {
			return nil, fmt.Errorf("embed chunk %s: %w", chunk.ID, err)
		}

		vectors = append(vectors, VectorRecord{
			ChunkID:    chunk.ID,
			Dimensions: len(embedding),
			Embedding:  EncodeEmbedding(embedding),
		})
	}

	return vectors, nil
}
