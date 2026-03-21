package openai

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/sidekickos/rillan/internal/config"
	internalopenai "github.com/sidekickos/rillan/internal/openai"
)

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func New(cfg config.OpenAIConfig, client *http.Client) *Client {
	if client == nil {
		client = http.DefaultClient
	}

	return &Client{
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:     cfg.APIKey,
		httpClient: client,
	}
}

func (c *Client) Name() string {
	return "openai"
}

func (c *Client) Ready(context.Context) error {
	return nil
}

func (c *Client) ChatCompletions(ctx context.Context, _ internalopenai.ChatCompletionRequest, body []byte) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create upstream request: %w", err)
	}

	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("perform upstream request: %w", err)
	}

	return response, nil
}
