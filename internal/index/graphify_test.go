package index

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rillanai/rillan/internal/config"
)

func TestRebuildIncludesGraphifyContentWhenEnabled(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	graphRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(graphRoot, "graph.json"), []byte(`{"nodes":[{"id":"AuthHandler","label":"AuthHandler","type":"class"}],"edges":[{"source":"AuthHandler","target":"PaymentFlow","type":"calls"}]}`), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(graphRoot, "wiki"), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(graphRoot, "wiki", "AuthHandler.md"), []byte("# AuthHandler\nCalls PaymentFlow.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))

	cfg := config.DefaultConfig()
	cfg.Index.Root = root
	cfg.KnowledgeGraph.Enabled = true
	cfg.KnowledgeGraph.Path = graphRoot

	if _, err := Rebuild(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("Rebuild returned error: %v", err)
	}

	store, err := OpenStore(DefaultDBPath())
	if err != nil {
		t.Fatalf("OpenStore returned error: %v", err)
	}
	defer store.Close()

	results, err := store.SearchChunksKeyword(context.Background(), "PaymentFlow", 10)
	if err != nil {
		t.Fatalf("SearchChunksKeyword returned error: %v", err)
	}
	found := false
	for _, result := range results {
		if strings.HasPrefix(result.DocumentPath, graphifyPrefix) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected Graphify chunk among results: %#v", results)
	}
}

func TestReadGraphifyStatus(t *testing.T) {
	graphRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(graphRoot, "graph.json"), []byte(`{"nodes":[{"id":"n1"},{"id":"n2"}],"edges":[{"source":"n1","target":"n2"}]}`), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	status, err := ReadGraphifyStatus(config.KnowledgeGraphConfig{Enabled: true, Path: graphRoot})
	if err != nil {
		t.Fatalf("ReadGraphifyStatus returned error: %v", err)
	}
	if !status.Present || status.Nodes != 2 || status.Edges != 1 || status.SHA256 == "" {
		t.Fatalf("unexpected status: %#v", status)
	}
}
