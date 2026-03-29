package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIndexCommandUsesReachableLocalModel(t *testing.T) {
	root := t.TempDir()
	mustWriteCommandTestFile(t, filepath.Join(root, "a.go"), "package main\n\nfunc main() {}\n")
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.WriteHeader(http.StatusOK)
		case "/api/embed":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float64{{0.1, 0.2, 0.3}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configPath := writeIndexTestConfig(t, root, server.URL, true)
	cmd := newIndexCommand()
	cmd.SetArgs([]string{"--config", configPath})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext returned error: %v", err)
	}
	if !strings.Contains(stdout.String(), "index complete") {
		t.Fatalf("output = %q, want index complete", stdout.String())
	}
}

func TestIndexCommandFailsWhenLocalModelConfiguredButUnreachable(t *testing.T) {
	root := t.TempDir()
	mustWriteCommandTestFile(t, filepath.Join(root, "a.go"), "package main\n\nfunc main() {}\n")
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))

	configPath := writeIndexTestConfig(t, root, "http://127.0.0.1:0", true)
	cmd := newIndexCommand()
	cmd.SetArgs([]string{"--config", configPath})

	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected unreachable local model to fail index command")
	}
	if !strings.Contains(err.Error(), "ollama unavailable") {
		t.Fatalf("error = %v, want ollama unavailable", err)
	}
}

func writeIndexTestConfig(t *testing.T, root string, baseURL string, enabled bool) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := "server:\n  host: \"127.0.0.1\"\n  port: 8420\n  log_level: \"info\"\n\nindex:\n  root: \"" + root + "\"\n\nlocal_model:\n  enabled: " + map[bool]string{true: "true", false: "false"}[enabled] + "\n  base_url: \"" + baseURL + "\"\n  embed_model: \"nomic-embed-text\"\n  query_rewrite:\n    enabled: false\n    model: \"qwen3:0.6b\"\n\nruntime:\n  vector_store_mode: \"embedded\"\n  local_model_base_url: \"" + baseURL + "\"\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	return path
}

func mustWriteCommandTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}
