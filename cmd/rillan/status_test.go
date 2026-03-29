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

	"github.com/sidekickos/rillan/internal/index"
)

func TestStatusCommandShowsCommittedAndFailedAttemptSeparately(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))

	configPath := writeStatusTestConfig(t)
	store, err := index.OpenStore(index.DefaultDBPath())
	if err != nil {
		t.Fatalf("OpenStore returned error: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	runID, err := store.RecordRunStart(ctx, "/committed-root")
	if err != nil {
		t.Fatalf("RecordRunStart returned error: %v", err)
	}
	if err := store.ReplaceAll(ctx,
		[]index.DocumentRecord{{Path: "a.go", ContentHash: "hash", SizeBytes: 10}},
		[]index.ChunkRecord{{ID: "chunk", DocumentPath: "a.go", Ordinal: 0, StartLine: 1, EndLine: 1, Content: "package main", ContentHash: "hash2"}},
		[]index.VectorRecord{{ChunkID: "chunk", Dimensions: 8, Embedding: []byte{1, 2, 3, 4}}},
	); err != nil {
		t.Fatalf("ReplaceAll returned error: %v", err)
	}
	if err := store.RecordRunCompletion(ctx, runID, index.RunStatusSucceeded, 1, 1, 1, ""); err != nil {
		t.Fatalf("RecordRunCompletion returned error: %v", err)
	}

	failedRunID, err := store.RecordRunStart(ctx, "/failed-root")
	if err != nil {
		t.Fatalf("RecordRunStart returned error: %v", err)
	}
	if err := store.RecordRunCompletion(ctx, failedRunID, index.RunStatusFailed, 0, 0, 0, "boom"); err != nil {
		t.Fatalf("RecordRunCompletion returned error: %v", err)
	}

	cmd := newStatusCommand()
	cmd.SetArgs([]string{"--config", configPath})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext returned error: %v", err)
	}

	output := stdout.String()
	for _, want := range []string{
		"configured_root: /configured-root",
		"last_attempt_state: failed",
		"last_attempt_root: /failed-root",
		"last_attempt_error: boom",
		"committed_root: /committed-root",
		"documents: 1",
		"chunks: 1",
		"vectors: 1",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestStatusCommandReportsReachableLocalModel(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/" {
			_, _ = w.Write([]byte("ok"))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float64{{0.1}}})
	}))
	defer server.Close()

	configPath := writeLocalModelStatusConfig(t, server.URL)
	cmd := newStatusCommand()
	cmd.SetArgs([]string{"--config", configPath})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext returned error: %v", err)
	}

	output := stdout.String()
	for _, want := range []string{"local_model_enabled: true", "local_model_reachable: true", "local_model_url: " + server.URL} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestStatusCommandReportsUnreachableLocalModel(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))

	configPath := writeLocalModelStatusConfig(t, "http://127.0.0.1:0")
	cmd := newStatusCommand()
	cmd.SetArgs([]string{"--config", configPath})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext returned error: %v", err)
	}

	output := stdout.String()
	for _, want := range []string{"local_model_enabled: true", "local_model_reachable: false"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func writeStatusTestConfig(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `server:
  host: "127.0.0.1"
  port: 8420
  log_level: "info"

index:
  root: "/configured-root"

runtime:
  vector_store_mode: "embedded"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	return path
}

func writeLocalModelStatusConfig(t *testing.T, baseURL string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `server:
  host: "127.0.0.1"
  port: 8420
  log_level: "info"

index:
  root: "/configured-root"

local_model:
  enabled: true
  base_url: "` + baseURL + `"
  embed_model: "nomic-embed-text"
  query_rewrite:
    enabled: true
    model: "qwen3:0.6b"

runtime:
  vector_store_mode: "embedded"
  local_model_base_url: "` + baseURL + `"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	return path
}
