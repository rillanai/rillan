package anthropic

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sidekickos/rillan/internal/config"
	internalopenai "github.com/sidekickos/rillan/internal/openai"
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
	resp, err := client.ChatCompletions(context.Background(), internalopenai.ChatCompletionRequest{
		Model: "claude-sonnet-4-5",
		Messages: []internalopenai.Message{
			{Role: "system", Content: []byte(`"Keep answers terse."`)},
			{Role: "developer", Content: []byte(`"Use markdown."`)},
			{Role: "user", Content: []byte(`"ping"`)},
			{Role: "assistant", Content: []byte(`"pong"`)},
		},
	}, nil)
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

func TestChatCompletionsRejectsUnsupportedRoles(t *testing.T) {
	t.Parallel()

	client := New(config.AnthropicConfig{BaseURL: "https://api.anthropic.com", APIKey: "anthropic-key"}, nil)
	_, err := client.ChatCompletions(context.Background(), internalopenai.ChatCompletionRequest{
		Model:    "claude-sonnet-4-5",
		Messages: []internalopenai.Message{{Role: "tool", Content: []byte(`"result"`)}},
	}, nil)
	if err == nil {
		t.Fatal("expected ChatCompletions to reject tool-role messages")
	}
}
