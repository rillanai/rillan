// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package retrieval

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rillanai/rillan/internal/ollama"
)

func TestOllamaQueryRewriterReturnsRewrittenQuery(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": "improved search query",
		})
	}))
	defer server.Close()

	client := ollama.New(server.URL, server.Client())
	rewriter := NewOllamaQueryRewriter(client, "qwen3:0.6b")

	result, err := rewriter.Rewrite(context.Background(), "how does auth work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "improved search query" {
		t.Fatalf("expected 'improved search query', got %q", result)
	}
}

func TestOllamaQueryRewriterReturnsOriginalOnEmptyResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": "  ",
		})
	}))
	defer server.Close()

	client := ollama.New(server.URL, server.Client())
	rewriter := NewOllamaQueryRewriter(client, "qwen3:0.6b")

	result, err := rewriter.Rewrite(context.Background(), "original query")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "original query" {
		t.Fatalf("expected 'original query', got %q", result)
	}
}

func TestOllamaQueryRewriterReturnsErrorOnFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := ollama.New(server.URL, server.Client())
	rewriter := NewOllamaQueryRewriter(client, "qwen3:0.6b")

	_, err := rewriter.Rewrite(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error on server failure")
	}
}
