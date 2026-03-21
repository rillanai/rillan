package providers

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/sidekickos/rillan/internal/config"
	internalopenai "github.com/sidekickos/rillan/internal/openai"
	provideropenai "github.com/sidekickos/rillan/internal/providers/openai"
)

type Provider interface {
	Name() string
	Ready(context.Context) error
	ChatCompletions(context.Context, internalopenai.ChatCompletionRequest, []byte) (*http.Response, error)
}

func New(cfg config.ProviderConfig, client *http.Client) (Provider, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Type)) {
	case config.ProviderOpenAI:
		return provideropenai.New(cfg.OpenAI, client), nil
	case config.ProviderAnthropic:
		return nil, fmt.Errorf("anthropic is intentionally not implemented in milestone one; use the openai provider path instead")
	default:
		return nil, fmt.Errorf("unsupported provider type %q", cfg.Type)
	}
}
