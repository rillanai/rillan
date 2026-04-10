// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package ollama_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rillanai/rillan/internal/ollama"
)

func TestEmbed(t *testing.T) {
	t.Parallel()

	t.Run("returns float32 embeddings", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if r.URL.Path != "/api/embed" {
				t.Errorf("expected /api/embed, got %s", r.URL.Path)
			}

			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if req["model"] != "nomic-embed-text" {
				t.Errorf("expected model nomic-embed-text, got %v", req["model"])
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"embeddings": [][]float64{{0.1, 0.2, 0.3, 0.4}},
			})
		}))
		defer server.Close()

		client := ollama.New(server.URL, server.Client())
		result, err := client.Embed(context.Background(), "nomic-embed-text", "hello world")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result) != 4 {
			t.Fatalf("expected 4 dimensions, got %d", len(result))
		}
		if result[0] < 0.09 || result[0] > 0.11 {
			t.Errorf("expected ~0.1, got %f", result[0])
		}
	})

	t.Run("returns error on non-200 status", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"model not found"}`))
		}))
		defer server.Close()

		client := ollama.New(server.URL, server.Client())
		_, err := client.Embed(context.Background(), "bad-model", "hello")
		if err == nil {
			t.Fatal("expected error for 500 response")
		}
	})

	t.Run("returns error on empty embeddings", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"embeddings": [][]float64{},
			})
		}))
		defer server.Close()

		client := ollama.New(server.URL, server.Client())
		_, err := client.Embed(context.Background(), "nomic-embed-text", "hello")
		if err == nil {
			t.Fatal("expected error for empty embeddings")
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"embeddings": [][]float64{{0.1}},
			})
		}))
		defer server.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		client := ollama.New(server.URL, server.Client())
		_, err := client.Embed(ctx, "nomic-embed-text", "hello")
		if err == nil {
			t.Fatal("expected error for cancelled context")
		}
	})
}

func TestGenerate(t *testing.T) {
	t.Parallel()

	t.Run("returns generated text", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if r.URL.Path != "/api/generate" {
				t.Errorf("expected /api/generate, got %s", r.URL.Path)
			}

			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if req["stream"] != false {
				t.Errorf("expected stream=false, got %v", req["stream"])
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"response": "rewritten query text",
			})
		}))
		defer server.Close()

		client := ollama.New(server.URL, server.Client())
		result, err := client.Generate(context.Background(), "qwen3:0.6b", "rewrite this query")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "rewritten query text" {
			t.Errorf("expected 'rewritten query text', got %q", result)
		}
	})

	t.Run("returns error on non-200 status", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid model"}`))
		}))
		defer server.Close()

		client := ollama.New(server.URL, server.Client())
		_, err := client.Generate(context.Background(), "bad", "prompt")
		if err == nil {
			t.Fatal("expected error for 400 response")
		}
	})
}

func TestPing(t *testing.T) {
	t.Parallel()

	t.Run("succeeds when server responds", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Errorf("expected GET, got %s", r.Method)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Ollama is running"))
		}))
		defer server.Close()

		client := ollama.New(server.URL, server.Client())
		if err := client.Ping(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("returns error when server unreachable", func(t *testing.T) {
		t.Parallel()

		client := ollama.New("http://127.0.0.1:0", &http.Client{})
		if err := client.Ping(context.Background()); err == nil {
			t.Fatal("expected error for unreachable server")
		}
	})

	t.Run("returns error on non-200 status", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer server.Close()

		client := ollama.New(server.URL, server.Client())
		if err := client.Ping(context.Background()); err == nil {
			t.Fatal("expected error for 503 response")
		}
	})
}
