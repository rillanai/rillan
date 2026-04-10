// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package index

import (
	"context"
	"path/filepath"
	"testing"
)

func TestStoreSearchChunksReturnsDeterministicTopK(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("OpenStore returned error: %v", err)
	}
	defer store.Close()

	chunks := []ChunkRecord{
		{ID: "chunk-1", DocumentPath: "alpha.txt", Ordinal: 0, StartLine: 1, EndLine: 1, Content: "alpha retrieval focus", ContentHash: "h1"},
		{ID: "chunk-2", DocumentPath: "beta.txt", Ordinal: 0, StartLine: 1, EndLine: 1, Content: "different content", ContentHash: "h2"},
		{ID: "chunk-3", DocumentPath: "alpha.txt", Ordinal: 1, StartLine: 2, EndLine: 2, Content: "alpha retrieval focus", ContentHash: "h3"},
	}
	vectors := make([]VectorRecord, 0, len(chunks))
	for _, chunk := range chunks {
		vectors = append(vectors, VectorRecord{
			ChunkID:    chunk.ID,
			Dimensions: 8,
			Embedding:  EncodeEmbedding(PlaceholderEmbedding(chunk.Content)),
		})
	}

	if err := store.ReplaceAll(context.Background(), []DocumentRecord{
		{Path: "alpha.txt", ContentHash: "dh1", SizeBytes: 10},
		{Path: "beta.txt", ContentHash: "dh2", SizeBytes: 10},
	}, chunks, vectors); err != nil {
		t.Fatalf("ReplaceAll returned error: %v", err)
	}

	results, err := store.SearchChunks(context.Background(), PlaceholderEmbedding("alpha retrieval focus"), 2)
	if err != nil {
		t.Fatalf("SearchChunks returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if got, want := results[0].ChunkID, "chunk-1"; got != want {
		t.Fatalf("first chunk = %q, want %q", got, want)
	}
	if got, want := results[1].ChunkID, "chunk-3"; got != want {
		t.Fatalf("second chunk = %q, want %q", got, want)
	}
}

func TestStoreSearchChunksKeywordReturnsKeywordMatches(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("OpenStore returned error: %v", err)
	}
	defer store.Close()

	chunks := []ChunkRecord{
		{ID: "chunk-1", DocumentPath: "alpha.txt", Ordinal: 0, StartLine: 1, EndLine: 1, Content: "alpha retrieval focus", ContentHash: "h1"},
		{ID: "chunk-2", DocumentPath: "beta.txt", Ordinal: 0, StartLine: 1, EndLine: 1, Content: "different content entirely", ContentHash: "h2"},
	}
	vectors := []VectorRecord{
		{ChunkID: "chunk-1", Dimensions: 8, Embedding: EncodeEmbedding(PlaceholderEmbedding(chunks[0].Content))},
		{ChunkID: "chunk-2", Dimensions: 8, Embedding: EncodeEmbedding(PlaceholderEmbedding(chunks[1].Content))},
	}

	if err := store.ReplaceAll(context.Background(), []DocumentRecord{
		{Path: "alpha.txt", ContentHash: "dh1", SizeBytes: 10},
		{Path: "beta.txt", ContentHash: "dh2", SizeBytes: 10},
	}, chunks, vectors); err != nil {
		t.Fatalf("ReplaceAll returned error: %v", err)
	}

	results, err := store.SearchChunksKeyword(context.Background(), "alpha retrieval", 2)
	if err != nil {
		t.Fatalf("SearchChunksKeyword returned error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected keyword search results")
	}
	if got, want := results[0].ChunkID, "chunk-1"; got != want {
		t.Fatalf("first keyword chunk = %q, want %q", got, want)
	}
}

func TestStoreSearchChunksRejectsDimensionMismatch(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("OpenStore returned error: %v", err)
	}
	defer store.Close()

	if err := store.ReplaceAll(context.Background(), []DocumentRecord{{Path: "alpha.txt", ContentHash: "dh1", SizeBytes: 10}}, []ChunkRecord{{
		ID:           "chunk-1",
		DocumentPath: "alpha.txt",
		Ordinal:      0,
		StartLine:    1,
		EndLine:      1,
		Content:      "alpha retrieval focus",
		ContentHash:  "h1",
	}}, []VectorRecord{{
		ChunkID:    "chunk-1",
		Dimensions: 3,
		Embedding:  EncodeEmbedding([]float32{0.1, 0.2, 0.3}),
	}}); err != nil {
		t.Fatalf("ReplaceAll returned error: %v", err)
	}

	if _, err := store.SearchChunks(context.Background(), PlaceholderEmbedding("alpha retrieval focus"), 1); err == nil {
		t.Fatal("expected dimension mismatch to fail search")
	}
}
