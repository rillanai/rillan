package index

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sidekickos/rillan/internal/config"
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
	mustWriteFile(t, filepath.Join(root, "skip.txt"), "skip")
	if err := os.WriteFile(filepath.Join(root, "image.bin"), []byte{0, 1, 2}, 0o644); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	files, err := DiscoverFiles(config.IndexConfig{Root: root, Excludes: []string{"skip.txt"}, ChunkSizeLines: 10})
	if err != nil {
		t.Fatalf("DiscoverFiles returned error: %v", err)
	}

	if len(files) != 1 || files[0].RelativePath != "keep.go" {
		t.Fatalf("unexpected discovered files: %#v", files)
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
