// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package anthropic

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rillanai/rillan/internal/chat"
	"github.com/rillanai/rillan/internal/config"
	"github.com/rillanai/rillan/internal/observability"
)

func TestChatCompletionsTranslatesRequestAndAppliesAnthropicHeaders(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotAPIKey string
	var gotVersion string
	var gotBody string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll returned error: %v", err)
		}
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_123","type":"message"}`))
	}))
	defer server.Close()

	client := New(config.AnthropicConfig{BaseURL: server.URL, APIKey: "anthropic-key"}, server.Client())
	resp, err := client.ChatCompletions(context.Background(), chat.ProviderRequest{Request: chat.Request{
		Model: "claude-sonnet-4-5",
		Messages: []chat.Message{
			{Role: "system", Content: []byte(`"Keep answers terse."`)},
			{Role: "developer", Content: []byte(`"Use markdown."`)},
			{Role: "user", Content: []byte(`"ping"`)},
			{Role: "assistant", Content: []byte(`"pong"`)},
		},
	}})
	if err != nil {
		t.Fatalf("ChatCompletions returned error: %v", err)
	}
	defer resp.Body.Close()

	if got, want := gotPath, "/v1/messages"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := gotAPIKey, "anthropic-key"; got != want {
		t.Fatalf("x-api-key = %q, want %q", got, want)
	}
	if got, want := gotVersion, apiVersion; got != want {
		t.Fatalf("anthropic-version = %q, want %q", got, want)
	}
	if got, want := gotBody, `{"model":"claude-sonnet-4-5","system":"Keep answers terse.\n\nUse markdown.","messages":[{"role":"user","content":"ping"},{"role":"assistant","content":"pong"}],"max_tokens":1024}`; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestChatCompletionsPropagatesRequestID(t *testing.T) {
	t.Parallel()

	var gotRequestID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequestID = r.Header.Get("X-Request-ID")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_123","type":"message"}`))
	}))
	defer server.Close()

	client := New(config.AnthropicConfig{BaseURL: server.URL, APIKey: "anthropic-key"}, server.Client())
	ctx := observability.WithRequestID(context.Background(), "req-123")
	resp, err := client.ChatCompletions(ctx, chat.ProviderRequest{Request: chat.Request{Model: "claude-sonnet-4-5", Messages: []chat.Message{{Role: "user", Content: []byte(`"ping"`)}}}})
	if err != nil {
		t.Fatalf("ChatCompletions returned error: %v", err)
	}
	defer resp.Body.Close()
	if got, want := gotRequestID, "req-123"; got != want {
		t.Fatalf("X-Request-ID = %q, want %q", got, want)
	}
}

func TestChatCompletionsRejectsUnsupportedRoles(t *testing.T) {
	t.Parallel()

	client := New(config.AnthropicConfig{BaseURL: "https://api.anthropic.com", APIKey: "anthropic-key"}, nil)
	_, err := client.ChatCompletions(context.Background(), chat.ProviderRequest{Request: chat.Request{
		Model:    "claude-sonnet-4-5",
		Messages: []chat.Message{{Role: "tool", Content: []byte(`"result"`)}},
	}})
	if err == nil {
		t.Fatal("expected ChatCompletions to reject tool-role messages")
	}
}

func TestReadyChecksModelsEndpoint(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotAPIKey string
	var gotVersion string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(config.AnthropicConfig{BaseURL: server.URL, APIKey: "anthropic-key"}, server.Client())
	if err := client.Ready(context.Background()); err != nil {
		t.Fatalf("Ready returned error: %v", err)
	}
	if got, want := gotPath, "/v1/models"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := gotAPIKey, "anthropic-key"; got != want {
		t.Fatalf("x-api-key = %q, want %q", got, want)
	}
	if got, want := gotVersion, apiVersion; got != want {
		t.Fatalf("anthropic-version = %q, want %q", got, want)
	}
}

func TestReadyReturnsErrorOnNonOKStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := New(config.AnthropicConfig{BaseURL: server.URL, APIKey: "anthropic-key"}, server.Client())
	if err := client.Ready(context.Background()); err == nil {
		t.Fatal("expected Ready to fail on non-200 status")
	}
}

func TestNewUsesBoundedDefaultClientTimeout(t *testing.T) {
	client := New(config.AnthropicConfig{BaseURL: "https://api.anthropic.com", APIKey: "anthropic-key"}, nil)
	if client.httpClient == nil {
		t.Fatal("expected httpClient to be set")
	}
	if client.httpClient.Timeout <= 0 {
		t.Fatalf("httpClient timeout = %v, want > 0", client.httpClient.Timeout)
	}
}
