package openai

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

func TestChatCompletionsForwardsAuthorizationAndBody(t *testing.T) {
	var gotAuth string
	var gotPath string
	var gotBody string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll returned error: %v", err)
		}
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_123","object":"chat.completion"}`))
	}))
	defer server.Close()

	client := New(config.OpenAIConfig{BaseURL: server.URL, APIKey: "test-key"}, server.Client())
	resp, err := client.ChatCompletions(context.Background(), chat.ProviderRequest{Request: chat.Request{}, RawBody: []byte(`{"model":"gpt-4o-mini"}`)})
	if err != nil {
		t.Fatalf("ChatCompletions returned error: %v", err)
	}
	defer resp.Body.Close()

	if got, want := gotAuth, "Bearer test-key"; got != want {
		t.Fatalf("Authorization header = %q, want %q", got, want)
	}
	if got, want := gotPath, "/chat/completions"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := gotBody, `{"model":"gpt-4o-mini"}`; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestChatCompletionsPropagatesRequestID(t *testing.T) {
	var gotRequestID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequestID = r.Header.Get("X-Request-ID")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_123","object":"chat.completion"}`))
	}))
	defer server.Close()

	client := New(config.OpenAIConfig{BaseURL: server.URL, APIKey: "test-key"}, server.Client())
	ctx := observability.WithRequestID(context.Background(), "req-123")
	resp, err := client.ChatCompletions(ctx, chat.ProviderRequest{Request: chat.Request{}, RawBody: []byte(`{"model":"gpt-4o-mini"}`)})
	if err != nil {
		t.Fatalf("ChatCompletions returned error: %v", err)
	}
	defer resp.Body.Close()
	if got, want := gotRequestID, "req-123"; got != want {
		t.Fatalf("X-Request-ID = %q, want %q", got, want)
	}
}

func TestReadyChecksModelsEndpoint(t *testing.T) {
	var gotAuth string
	var gotPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(config.OpenAIConfig{BaseURL: server.URL, APIKey: "test-key"}, server.Client())
	if err := client.Ready(context.Background()); err != nil {
		t.Fatalf("Ready returned error: %v", err)
	}
	if got, want := gotAuth, "Bearer test-key"; got != want {
		t.Fatalf("Authorization header = %q, want %q", got, want)
	}
	if got, want := gotPath, "/models"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestReadyReturnsErrorOnNonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := New(config.OpenAIConfig{BaseURL: server.URL, APIKey: "test-key"}, server.Client())
	if err := client.Ready(context.Background()); err == nil {
		t.Fatal("expected Ready to fail on non-200 status")
	}
}

func TestNewUsesBoundedDefaultClientTimeout(t *testing.T) {
	client := New(config.OpenAIConfig{BaseURL: "https://example.com/v1", APIKey: "test-key"}, nil)
	if client.httpClient == nil {
		t.Fatal("expected httpClient to be set")
	}
	if client.httpClient.Timeout <= 0 {
		t.Fatalf("httpClient timeout = %v, want > 0", client.httpClient.Timeout)
	}
}
