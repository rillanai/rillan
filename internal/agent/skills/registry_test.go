// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package skills

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/rillanai/rillan/internal/index"
)

func TestRegistryReadFilesReturnsBoundedContent(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(repo, "docs", "guide.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte("hello world from file"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	registry := NewRegistry([]string{repo})
	result, err := registry.ReadFiles(context.Background(), ReadFilesRequest{RepoRoot: repo, Paths: []string{"docs/guide.md"}, MaxFiles: 2, MaxCharsPerFile: 5})
	if err != nil {
		t.Fatalf("ReadFiles returned error: %v", err)
	}
	if got, want := len(result.Files), 1; got != want {
		t.Fatalf("files len = %d, want %d", got, want)
	}
	if got := result.Files[0].Content; got == "hello world from file" {
		t.Fatalf("expected bounded content, got %q", got)
	}
}

func TestRegistrySearchRepoReturnsMatches(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(repo, "docs", "guide.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte("retrieval context is useful"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	registry := NewRegistry([]string{repo})
	result, err := registry.SearchRepo(context.Background(), SearchRepoRequest{RepoRoot: repo, Query: "context", MaxMatches: 3, MaxSnippetChars: 40})
	if err != nil {
		t.Fatalf("SearchRepo returned error: %v", err)
	}
	if got, want := len(result.Matches), 1; got != want {
		t.Fatalf("matches len = %d, want %d", got, want)
	}
}

func TestRegistryIndexLookupReturnsMatches(t *testing.T) {
	store, err := index.OpenStore(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("OpenStore returned error: %v", err)
	}
	defer store.Close()
	if err := store.ReplaceAll(context.Background(), []index.DocumentRecord{{Path: "docs/guide.md", ContentHash: "h1", SizeBytes: 10}}, []index.ChunkRecord{{ID: "chunk-1", DocumentPath: "docs/guide.md", Ordinal: 0, StartLine: 1, EndLine: 2, Content: "context package builder", ContentHash: "c1"}}, []index.VectorRecord{{ChunkID: "chunk-1", Dimensions: 8, Embedding: index.EncodeEmbedding(index.PlaceholderEmbedding("context package builder"))}}); err != nil {
		t.Fatalf("ReplaceAll returned error: %v", err)
	}

	registry := NewRegistry(nil)
	result, err := registry.IndexLookup(context.Background(), IndexLookupRequest{DBPath: store.Path(), Query: "context", MaxMatches: 2, MaxSnippetChars: 40})
	if err != nil {
		t.Fatalf("IndexLookup returned error: %v", err)
	}
	if got, want := len(result.Matches), 1; got != want {
		t.Fatalf("matches len = %d, want %d", got, want)
	}
}

func TestRegistryGitCommandsReturnStructuredResults(t *testing.T) {
	repo := t.TempDir()
	stubGit(t, func(ctx context.Context, root string, args ...string) ([]byte, error) {
		switch args[0] {
		case "status":
			return []byte(" M internal/agent/context_package.go\n?? internal/agent/skills/read_only.go\n"), nil
		case "diff":
			return []byte("diff --git a/file b/file\n+hello\n"), nil
		default:
			return nil, nil
		}
	})

	registry := NewRegistry([]string{repo})
	status, err := registry.GitStatus(context.Background(), GitStatusRequest{RepoRoot: repo, MaxEntries: 5})
	if err != nil {
		t.Fatalf("GitStatus returned error: %v", err)
	}
	if got, want := len(status.Entries), 2; got != want {
		t.Fatalf("status entries len = %d, want %d", got, want)
	}

	diff, err := registry.GitDiff(context.Background(), GitDiffRequest{RepoRoot: repo, MaxChars: 10})
	if err != nil {
		t.Fatalf("GitDiff returned error: %v", err)
	}
	if got := diff.Diff; got == "diff --git a/file b/file\n+hello\n" {
		t.Fatalf("expected bounded diff output, got %q", got)
	}
}

func TestRegistryExecuteDispatchesReadOnlyTool(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(repo, "docs", "guide.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte("hello world from file"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	registry := NewRegistry([]string{repo})
	result, err := registry.Execute(context.Background(), ExecuteRequest{Name: ToolNameReadFiles, RepoRoot: repo, Paths: []string{"docs/guide.md"}})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if got, want := result.Name, ToolNameReadFiles; got != want {
		t.Fatalf("result name = %q, want %q", got, want)
	}
	payload, ok := result.Payload.(ReadFilesResult)
	if !ok {
		t.Fatalf("payload type = %T, want ReadFilesResult", result.Payload)
	}
	if got, want := len(payload.Files), 1; got != want {
		t.Fatalf("files len = %d, want %d", got, want)
	}
}

func stubGit(t *testing.T, fn func(context.Context, string, ...string) ([]byte, error)) {
	t.Helper()
	original := gitCommand
	gitCommand = fn
	t.Cleanup(func() { gitCommand = original })
}

func TestRegistryGitStatusRejectsUnapprovedRepoRoot(t *testing.T) {
	repo := t.TempDir()
	registry := NewRegistry(nil)

	if _, err := registry.GitStatus(context.Background(), GitStatusRequest{RepoRoot: repo, MaxEntries: 5}); err == nil {
		t.Fatal("expected GitStatus to reject unapproved repo root")
	}
}

func TestRegistryReadFilesRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior is platform-specific")
	}

	repo := t.TempDir()
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsidePath, []byte("outside secret"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	linkPath := filepath.Join(repo, "docs", "escape.txt")
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.Symlink(outsidePath, linkPath); err != nil {
		t.Fatalf("Symlink returned error: %v", err)
	}

	registry := NewRegistry([]string{repo})
	if _, err := registry.ReadFiles(context.Background(), ReadFilesRequest{RepoRoot: repo, Paths: []string{"docs/escape.txt"}, MaxFiles: 1, MaxCharsPerFile: 40}); err == nil {
		t.Fatal("expected ReadFiles to reject symlink escape")
	}
}

func TestRegistrySearchRepoSkipsSymlinkedOutsideFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior is platform-specific")
	}

	repo := t.TempDir()
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsidePath, []byte("outside needle"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	insidePath := filepath.Join(repo, "docs", "guide.md")
	if err := os.MkdirAll(filepath.Dir(insidePath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(insidePath, []byte("inside needle"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	linkPath := filepath.Join(repo, "docs", "escape.txt")
	if err := os.Symlink(outsidePath, linkPath); err != nil {
		t.Fatalf("Symlink returned error: %v", err)
	}

	registry := NewRegistry([]string{repo})
	result, err := registry.SearchRepo(context.Background(), SearchRepoRequest{RepoRoot: repo, Query: "needle", MaxMatches: 5, MaxSnippetChars: 40})
	if err != nil {
		t.Fatalf("SearchRepo returned error: %v", err)
	}
	if got, want := len(result.Matches), 1; got != want {
		t.Fatalf("matches len = %d, want %d", got, want)
	}
	if got, want := result.Matches[0].Path, filepath.Join("docs", "guide.md"); got != want {
		t.Fatalf("match path = %q, want %q", got, want)
	}
}

func TestRegistrySearchRepoIncludesSymlinkedInRepoFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior is platform-specific")
	}

	repo := t.TempDir()
	insidePath := filepath.Join(repo, "docs", "guide.md")
	if err := os.MkdirAll(filepath.Dir(insidePath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(insidePath, []byte("inside needle"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	linkPath := filepath.Join(repo, "docs", "guide-link.md")
	if err := os.Symlink(insidePath, linkPath); err != nil {
		t.Fatalf("Symlink returned error: %v", err)
	}

	registry := NewRegistry([]string{repo})
	result, err := registry.SearchRepo(context.Background(), SearchRepoRequest{RepoRoot: repo, Query: "needle", MaxMatches: 5, MaxSnippetChars: 40})
	if err != nil {
		t.Fatalf("SearchRepo returned error: %v", err)
	}
	if got, want := len(result.Matches), 2; got != want {
		t.Fatalf("matches len = %d, want %d", got, want)
	}
}
