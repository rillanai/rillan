// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package index

import (
	"context"
	"errors"
	"testing"
)

func TestNewVectorStoreDefaultsToEmbedded(t *testing.T) {
	store, err := NewVectorStore("")
	if err != nil {
		t.Fatalf("NewVectorStore returned error: %v", err)
	}
	if got, want := store.Mode(), VectorStoreModeEmbedded; got != want {
		t.Fatalf("mode = %q, want %q", got, want)
	}
}

func TestEmbeddedVectorStoreBuildRecordsDeterministically(t *testing.T) {
	store := EmbeddedVectorStore{}
	chunks := []ChunkRecord{{ID: "chunk-1", Content: "hello"}}

	first, err := store.BuildRecords(context.Background(), chunks)
	if err != nil {
		t.Fatalf("BuildRecords returned error: %v", err)
	}
	second, err := store.BuildRecords(context.Background(), chunks)
	if err != nil {
		t.Fatalf("BuildRecords returned error: %v", err)
	}

	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("unexpected vector count: %d %d", len(first), len(second))
	}
	if string(first[0].Embedding) != string(second[0].Embedding) {
		t.Fatal("expected embedded vector records to be deterministic")
	}
}

func TestOllamaVectorStoreBuildRecordsUsesEmbedder(t *testing.T) {
	embedder := &fakeEmbedder{embedding: []float32{0.1, 0.2, 0.3}}
	store := NewOllamaVectorStore(embedder, "test-model")

	chunks := []ChunkRecord{
		{ID: "chunk-1", Content: "hello"},
		{ID: "chunk-2", Content: "world"},
	}

	records, err := store.BuildRecords(context.Background(), chunks)
	if err != nil {
		t.Fatalf("BuildRecords returned error: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if records[0].Dimensions != 3 {
		t.Fatalf("expected 3 dimensions, got %d", records[0].Dimensions)
	}
	if embedder.callCount != 2 {
		t.Fatalf("expected 2 embed calls, got %d", embedder.callCount)
	}
	if embedder.lastModel != "test-model" {
		t.Fatalf("expected model 'test-model', got %q", embedder.lastModel)
	}
}

func TestOllamaVectorStoreReturnsMode(t *testing.T) {
	store := NewOllamaVectorStore(nil, "")
	if got, want := store.Mode(), VectorStoreModeOllama; got != want {
		t.Fatalf("mode = %q, want %q", got, want)
	}
}

func TestOllamaVectorStoreReturnsEmbedError(t *testing.T) {
	embedder := &fakeEmbedder{err: errors.New("model not loaded")}
	store := NewOllamaVectorStore(embedder, "test-model")

	_, err := store.BuildRecords(context.Background(), []ChunkRecord{{ID: "chunk-1", Content: "hello"}})
	if err == nil {
		t.Fatal("expected error from embedder")
	}
}

func TestOllamaVectorStoreHonorsContextCancellation(t *testing.T) {
	embedder := &fakeEmbedder{embedding: []float32{0.1}}
	store := NewOllamaVectorStore(embedder, "test-model")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := store.BuildRecords(ctx, []ChunkRecord{{ID: "chunk-1", Content: "hello"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

type fakeEmbedder struct {
	embedding []float32
	err       error
	callCount int
	lastModel string
}

func (f *fakeEmbedder) Embed(_ context.Context, model string, _ string) ([]float32, error) {
	f.callCount++
	f.lastModel = model
	if f.err != nil {
		return nil, f.err
	}
	return f.embedding, nil
}

func TestEmbeddedVectorStoreHonorsContextCancellation(t *testing.T) {
	store := EmbeddedVectorStore{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := store.BuildRecords(ctx, []ChunkRecord{{ID: "chunk-1", Content: "hello"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}
