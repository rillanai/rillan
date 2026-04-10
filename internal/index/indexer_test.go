// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package index

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/rillanai/rillan/internal/config"
)

func TestIndexerRebuildsFromScratch(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "a.go"), "package main\n\nfunc main() {}\n")
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))

	cfg := config.DefaultConfig()
	cfg.Index.Root = root
	status, err := Rebuild(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("Rebuild returned error: %v", err)
	}
	if status.Documents != 1 {
		t.Fatalf("documents = %d, want 1", status.Documents)
	}
	if status.Chunks == 0 || status.Vectors == 0 {
		t.Fatalf("expected chunks and vectors to be created: %#v", status)
	}
}

func TestIndexerRemovesDeletedFilesOnReindex(t *testing.T) {
	root := t.TempDir()
	aPath := filepath.Join(root, "a.go")
	bPath := filepath.Join(root, "b.go")
	mustWriteFile(t, aPath, "package main\n")
	mustWriteFile(t, bPath, "package main\n")
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))

	cfg := config.DefaultConfig()
	cfg.Index.Root = root
	if _, err := Rebuild(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("first Rebuild returned error: %v", err)
	}
	if err := os.Remove(bPath); err != nil {
		t.Fatalf("Remove returned error: %v", err)
	}

	status, err := Rebuild(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("second Rebuild returned error: %v", err)
	}
	if got, want := status.Documents, 1; got != want {
		t.Fatalf("documents = %d, want %d", got, want)
	}
}

func TestReadStatusFallsBackToConfiguredRootBeforeFirstIndex(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))

	cfg := config.DefaultConfig()
	cfg.Index.Root = filepath.Join(t.TempDir(), "vault")

	status, err := ReadStatus(context.Background(), cfg)
	if err != nil {
		t.Fatalf("ReadStatus returned error: %v", err)
	}
	if got, want := status.ConfiguredRootPath, cfg.Index.Root; got != want {
		t.Fatalf("configured root path = %q, want %q", got, want)
	}
	if got, want := status.LastAttemptState, RunStatusNeverIndexed; got != want {
		t.Fatalf("state = %q, want %q", got, want)
	}
	if got := status.CommittedRootPath; got != "" {
		t.Fatalf("committed root path = %q, want empty", got)
	}
}
