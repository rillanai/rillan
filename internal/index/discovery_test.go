// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package index

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/rillanai/rillan/internal/config"
)

func TestDiscoverFilesReturnsDeterministicOrder(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "b.txt"), "second")
	mustWriteFile(t, filepath.Join(root, "a.txt"), "first")

	files, err := DiscoverFiles(config.IndexConfig{Root: root, Excludes: config.DefaultConfig().Index.Excludes, ChunkSizeLines: 10})
	if err != nil {
		t.Fatalf("DiscoverFiles returned error: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("files count = %d, want 2", len(files))
	}
	if files[0].RelativePath != "a.txt" || files[1].RelativePath != "b.txt" {
		t.Fatalf("unexpected file order: %#v", files)
	}
}

func TestDiscoverFilesSkipsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior is not part of the current release targets")
	}

	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "outside.txt")
	mustWriteFile(t, target, "outside")
	if err := os.Symlink(target, filepath.Join(root, "linked.txt")); err != nil {
		t.Fatalf("Symlink returned error: %v", err)
	}

	files, err := DiscoverFiles(config.IndexConfig{Root: root, ChunkSizeLines: 10})
	if err != nil {
		t.Fatalf("DiscoverFiles returned error: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected symlink target to be skipped, got %#v", files)
	}
}

func TestDiscoverFilesSkipsExcludedAndBinaryFiles(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "keep.go"), "package main\n")
	mustWriteFile(t, filepath.Join(root, "nested", "keep.go"), "package nested\n")
	mustWriteFile(t, filepath.Join(root, "skip.txt"), "skip")
	if err := os.WriteFile(filepath.Join(root, "image.bin"), []byte{0, 1, 2}, 0o644); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	files, err := DiscoverFiles(config.IndexConfig{Root: root, Includes: []string{"*.go"}, Excludes: []string{"skip.txt"}, ChunkSizeLines: 10})
	if err != nil {
		t.Fatalf("DiscoverFiles returned error: %v", err)
	}

	if len(files) != 2 || files[0].RelativePath != "keep.go" || files[1].RelativePath != "nested/keep.go" {
		t.Fatalf("unexpected discovered files: %#v", files)
	}
}

func TestDiscoverFilesSupportsRecursiveGlobPatterns(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "docs", "guide.md"), "guide")
	mustWriteFile(t, filepath.Join(root, "nested", "docs", "notes.md"), "notes")
	mustWriteFile(t, filepath.Join(root, "nested", "docs", "skip.txt"), "skip")

	files, err := DiscoverFiles(config.IndexConfig{Root: root, Includes: []string{"**/*.md"}, ChunkSizeLines: 10})
	if err != nil {
		t.Fatalf("DiscoverFiles returned error: %v", err)
	}

	if len(files) != 2 || files[0].RelativePath != "docs/guide.md" || files[1].RelativePath != "nested/docs/notes.md" {
		t.Fatalf("unexpected discovered files: %#v", files)
	}
}

func TestDiscoverFilesRequiresDirectoryRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "file.txt")
	mustWriteFile(t, root, "content")

	_, err := DiscoverFiles(config.IndexConfig{Root: root, ChunkSizeLines: 10})
	if err == nil {
		t.Fatal("expected non-directory root to fail")
	}
}

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}
