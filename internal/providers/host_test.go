package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rillanai/rillan/internal/chat"
	"github.com/rillanai/rillan/internal/config"
	internalopenai "github.com/rillanai/rillan/internal/openai"
)

type observedRequest struct {
	method        string
	path          string
	authorization string
	body          string
}

func TestNewHostDefaultProviderUsesOpenAIAdapter(t *testing.T) {
	t.Parallel()

	requests := make(chan observedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		requests <- observedRequest{
			method:        r.Method,
			path:          r.URL.Path,
			authorization: r.Header.Get("Authorization"),
			body:          string(body),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"ok"}`))
	}))
	defer server.Close()

	host, err := NewHost(config.RuntimeProviderHostConfig{
		Default: "work-gpt",
		Providers: []config.RuntimeProviderAdapterConfig{{
			ID:   "work-gpt",
			Type: config.ProviderOpenAI,
			OpenAI: config.OpenAIConfig{
				BaseURL: server.URL,
				APIKey:  "secret-key",
			},
		}},
	}, server.Client())
	if err != nil {
		t.Fatalf("NewHost returned error: %v", err)
	}

	provider, err := host.DefaultProvider()
	if err != nil {
		t.Fatalf("DefaultProvider returned error: %v", err)
	}

	response, err := provider.ChatCompletions(context.Background(), chat.ProviderRequest{Request: internalopenai.ChatCompletionRequest{Model: "gpt-4o-mini"}, RawBody: []byte(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`)})
	if err != nil {
		t.Fatalf("ChatCompletions returned error: %v", err)
	}
	defer response.Body.Close()

	if got, want := provider.Name(), "openai"; got != want {
		t.Fatalf("provider.Name() = %q, want %q", got, want)
	}
	if got, want := response.StatusCode, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}

	request := <-requests
	if got, want := request.method, http.MethodPost; got != want {
		t.Fatalf("method = %q, want %q", got, want)
	}
	if got, want := request.path, "/chat/completions"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := request.authorization, "Bearer secret-key"; got != want {
		t.Fatalf("authorization = %q, want %q", got, want)
	}
	if got, want := request.body, `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`; got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
}

func TestNewHostRejectsUnknownDefaultProvider(t *testing.T) {
	t.Parallel()

	_, err := NewHost(config.RuntimeProviderHostConfig{
		Default: "missing",
		Providers: []config.RuntimeProviderAdapterConfig{{
			ID:   "work-gpt",
			Type: config.ProviderOpenAI,
			OpenAI: config.OpenAIConfig{
				BaseURL: "https://api.openai.com/v1",
			},
		}},
	}, nil)
	if err == nil {
		t.Fatal("expected NewHost to reject a missing default provider")
	}
}

func TestNewHostSupportsOpenAICompatibleBundledPresets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		providerID string
		preset     string
		apiKey     string
	}{
		{name: "xai", providerID: "xai-work", preset: config.LLMPresetXAI, apiKey: "xai-secret"},
		{name: "deepseek", providerID: "deepseek-work", preset: config.LLMPresetDeepSeek, apiKey: "deepseek-secret"},
	}

	for _, tt := range tests {
		tc := tt
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan observedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				requests <- observedRequest{
					method:        r.Method,
					path:          r.URL.Path,
					authorization: r.Header.Get("Authorization"),
					body:          string(body),
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id":"ok"}`))
			}))
			defer server.Close()

			host, err := NewHost(config.RuntimeProviderHostConfig{
				Default: tc.providerID,
				Providers: []config.RuntimeProviderAdapterConfig{{
					ID:     tc.providerID,
					Preset: tc.preset,
					Type:   config.ProviderOpenAICompatible,
					OpenAI: config.OpenAIConfig{
						BaseURL: server.URL,
						APIKey:  tc.apiKey,
					},
				}},
			}, server.Client())
			if err != nil {
				t.Fatalf("NewHost returned error: %v", err)
			}

			provider, err := host.DefaultProvider()
			if err != nil {
				t.Fatalf("DefaultProvider returned error: %v", err)
			}

			response, err := provider.ChatCompletions(context.Background(), chat.ProviderRequest{Request: internalopenai.ChatCompletionRequest{Model: "test-model"}, RawBody: []byte(`{"model":"test-model","messages":[{"role":"user","content":"ping"}]}`)})
			if err != nil {
				t.Fatalf("ChatCompletions returned error: %v", err)
			}
			defer response.Body.Close()

			request := <-requests
			if got, want := request.authorization, "Bearer "+tc.apiKey; got != want {
				t.Fatalf("authorization = %q, want %q", got, want)
			}
			if got, want := request.path, "/chat/completions"; got != want {
				t.Fatalf("path = %q, want %q", got, want)
			}
		})
	}
}

func TestNewHostSupportsAnthropicBundledPreset(t *testing.T) {
	t.Parallel()

	requests := make(chan observedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		requests <- observedRequest{
			method:        r.Method,
			path:          r.URL.Path,
			authorization: r.Header.Get("x-api-key"),
			body:          string(body),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_123"}`))
	}))
	defer server.Close()

	host, err := NewHost(config.RuntimeProviderHostConfig{
		Default: "anthropic-work",
		Providers: []config.RuntimeProviderAdapterConfig{{
			ID:     "anthropic-work",
			Preset: config.LLMPresetAnthropic,
			Type:   config.ProviderAnthropic,
			Anthropic: config.AnthropicConfig{
				Enabled: true,
				BaseURL: server.URL,
				APIKey:  "anthropic-key",
			},
		}},
	}, server.Client())
	if err != nil {
		t.Fatalf("NewHost returned error: %v", err)
	}

	provider, err := host.DefaultProvider()
	if err != nil {
		t.Fatalf("DefaultProvider returned error: %v", err)
	}

	response, err := provider.ChatCompletions(context.Background(), chat.ProviderRequest{Request: internalopenai.ChatCompletionRequest{
		Model:    "claude-sonnet-4-5",
		Messages: []internalopenai.Message{{Role: "user", Content: []byte(`"ping"`)}},
	}, RawBody: []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"ping"}]}`)})
	if err != nil {
		t.Fatalf("ChatCompletions returned error: %v", err)
	}
	defer response.Body.Close()

	if got, want := provider.Name(), "anthropic"; got != want {
		t.Fatalf("provider.Name() = %q, want %q", got, want)
	}

	request := <-requests
	if got, want := request.method, http.MethodPost; got != want {
		t.Fatalf("method = %q, want %q", got, want)
	}
	if got, want := request.path, "/v1/messages"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := request.authorization, "anthropic-key"; got != want {
		t.Fatalf("x-api-key = %q, want %q", got, want)
	}
	if got, want := request.body, `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"ping"}],"max_tokens":1024}`; got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
}

func TestNewHostSupportsInternalOllamaProvider(t *testing.T) {
	t.Parallel()

	requests := make(chan observedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		requests <- observedRequest{
			method: r.Method,
			path:   r.URL.Path,
			body:   string(body),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"response":"pong"}`))
	}))
	defer server.Close()

	host, err := NewHost(config.RuntimeProviderHostConfig{
		Default: "local-chat",
		Providers: []config.RuntimeProviderAdapterConfig{{
			ID:   "local-chat",
			Type: config.ProviderOllama,
			LocalModel: config.LocalModelProvider{
				BaseURL: server.URL,
			},
		}},
	}, server.Client())
	if err != nil {
		t.Fatalf("NewHost returned error: %v", err)
	}

	provider, err := host.DefaultProvider()
	if err != nil {
		t.Fatalf("DefaultProvider returned error: %v", err)
	}

	response, err := provider.ChatCompletions(context.Background(), chat.ProviderRequest{Request: internalopenai.ChatCompletionRequest{
		Model: "qwen3:8b",
		Messages: []internalopenai.Message{
			{Role: "system", Content: []byte(`"stay concise"`)},
			{Role: "user", Content: []byte(`"ping"`)},
		},
	}})
	if err != nil {
		t.Fatalf("ChatCompletions returned error: %v", err)
	}
	defer response.Body.Close()

	if got, want := provider.Name(), "ollama"; got != want {
		t.Fatalf("provider.Name() = %q, want %q", got, want)
	}
	if got, want := response.StatusCode, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}

	request := <-requests
	if got, want := request.method, http.MethodPost; got != want {
		t.Fatalf("method = %q, want %q", got, want)
	}
	if got, want := request.path, "/api/generate"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := request.body, `{"model":"qwen3:8b","prompt":"system: stay concise\n\nuser: ping\n\nassistant:","stream":false}`; got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}

	var decoded struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got, want := decoded.Model, "qwen3:8b"; got != want {
		t.Fatalf("response model = %q, want %q", got, want)
	}
	if got, want := len(decoded.Choices), 1; got != want {
		t.Fatalf("choice count = %d, want %d", got, want)
	}
	if got, want := decoded.Choices[0].Message.Role, "assistant"; got != want {
		t.Fatalf("response role = %q, want %q", got, want)
	}
	if got, want := decoded.Choices[0].Message.Content, "pong"; got != want {
		t.Fatalf("response content = %q, want %q", got, want)
	}
}
