package providers

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/sidekickos/rillan/internal/config"
	internalopenai "github.com/sidekickos/rillan/internal/openai"
	provideranthropic "github.com/sidekickos/rillan/internal/providers/anthropic"
	providerollama "github.com/sidekickos/rillan/internal/providers/ollama"
	provideropenai "github.com/sidekickos/rillan/internal/providers/openai"
	providerstdio "github.com/sidekickos/rillan/internal/providers/stdio"
)

type Provider interface {
	Name() string
	Ready(context.Context) error
	ChatCompletions(context.Context, internalopenai.ChatCompletionRequest, []byte) (*http.Response, error)
}

func New(cfg config.ProviderConfig, client *http.Client) (Provider, error) {
	return newAdapter(config.RuntimeProviderAdapterConfig{
		ID:         "default",
		Type:       cfg.Type,
		Transport:  config.LLMTransportHTTP,
		OpenAI:     cfg.OpenAI,
		Anthropic:  cfg.Anthropic,
		LocalModel: cfg.Local,
	}, client)
}

func newAdapter(cfg config.RuntimeProviderAdapterConfig, client *http.Client) (Provider, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Transport)) {
	case "", config.LLMTransportHTTP:
	case config.LLMTransportSTDIO:
		return providerstdio.New(cfg.Command), nil
	default:
		return nil, fmt.Errorf("unsupported provider transport %q", cfg.Transport)
	}

	switch strings.ToLower(strings.TrimSpace(cfg.Type)) {
	case config.ProviderOpenAI:
		return provideropenai.New(cfg.OpenAI, client), nil
	case config.ProviderOpenAICompatible:
		return provideropenai.New(cfg.OpenAI, client), nil
	case config.ProviderAnthropic:
		return provideranthropic.New(cfg.Anthropic, client), nil
	case config.ProviderOllama:
		return providerollama.New(cfg.LocalModel, client), nil
	default:
		return nil, fmt.Errorf("unsupported provider type %q", cfg.Type)
	}
}
