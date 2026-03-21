package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sidekickos/rillan/internal/config"
	internalopenai "github.com/sidekickos/rillan/internal/openai"
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
	resp, err := client.ChatCompletions(context.Background(), internalopenai.ChatCompletionRequest{}, []byte(`{"model":"gpt-4o-mini"}`))
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
