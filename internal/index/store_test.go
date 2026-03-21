package index

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOpenStoreBootstrapsSchema(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("OpenStore returned error: %v", err)
	}
	defer store.Close()

	status, err := store.ReadStatus(context.Background())
	if err != nil {
		t.Fatalf("ReadStatus returned error: %v", err)
	}
	if got, want := status.State, RunStatusNeverIndexed; got != want {
		t.Fatalf("state = %q, want %q", got, want)
	}
}

func TestOpenStoreIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.db")
	first, err := OpenStore(path)
	if err != nil {
		t.Fatalf("OpenStore returned error: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	second, err := OpenStore(path)
	if err != nil {
		t.Fatalf("second OpenStore returned error: %v", err)
	}
	defer second.Close()
}

func TestReadStatusKeepsCommittedCountsAfterFailedRun(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("OpenStore returned error: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	runID, err := store.RecordRunStart(ctx, "/root")
	if err != nil {
		t.Fatalf("RecordRunStart returned error: %v", err)
	}
	if err := store.ReplaceAll(ctx,
		[]DocumentRecord{{Path: "a.go", ContentHash: "hash", SizeBytes: 10}},
		[]ChunkRecord{{ID: "chunk", DocumentPath: "a.go", Ordinal: 0, StartLine: 1, EndLine: 1, Content: "package main", ContentHash: "hash2"}},
		[]VectorRecord{{ChunkID: "chunk", Dimensions: 8, Embedding: []byte{1, 2, 3, 4}}},
	); err != nil {
		t.Fatalf("ReplaceAll returned error: %v", err)
	}
	if err := store.RecordRunCompletion(ctx, runID, RunStatusSucceeded, 1, 1, 1, ""); err != nil {
		t.Fatalf("RecordRunCompletion returned error: %v", err)
	}

	failedRunID, err := store.RecordRunStart(ctx, "/other-root")
	if err != nil {
		t.Fatalf("RecordRunStart returned error: %v", err)
	}
	if err := store.RecordRunCompletion(ctx, failedRunID, RunStatusFailed, 0, 0, 0, "boom"); err != nil {
		t.Fatalf("RecordRunCompletion returned error: %v", err)
	}

	status, err := store.ReadStatus(ctx)
	if err != nil {
		t.Fatalf("ReadStatus returned error: %v", err)
	}
	if got, want := status.Documents, 1; got != want {
		t.Fatalf("documents = %d, want %d", got, want)
	}
	if got, want := status.State, RunStatusFailed; got != want {
		t.Fatalf("state = %q, want %q", got, want)
	}
	if got, want := status.RootPath, "/root"; got != want {
		t.Fatalf("root path = %q, want %q", got, want)
	}
}
