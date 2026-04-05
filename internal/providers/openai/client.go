package openai

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rillanai/rillan/internal/chat"
	"github.com/rillanai/rillan/internal/config"
	"github.com/rillanai/rillan/internal/observability"
)

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

const defaultHTTPTimeout = 30 * time.Second

func New(cfg config.OpenAIConfig, client *http.Client) *Client {
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
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

func (c *Client) Ready(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", nil)
	if err != nil {
		return fmt.Errorf("create readiness request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Accept", "application/json")
	if requestID := observability.RequestIDFromContext(ctx); requestID != "" {
		request.Header.Set("X-Request-ID", requestID)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("perform readiness request: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("openai readiness returned status %d", response.StatusCode)
	}
	return nil
}

func (c *Client) ChatCompletions(ctx context.Context, req chat.ProviderRequest) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(req.RawBody))
	if err != nil {
		return nil, fmt.Errorf("create upstream request: %w", err)
	}

	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if requestID := observability.RequestIDFromContext(ctx); requestID != "" {
		request.Header.Set("X-Request-ID", requestID)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("perform upstream request: %w", err)
	}

	return response, nil
}
