// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rillanai/rillan/internal/chat"
	"github.com/rillanai/rillan/internal/config"
	"github.com/rillanai/rillan/internal/observability"
)

const (
	apiVersion         = "2023-06-01"
	defaultMaxTokens   = 1024
	defaultHTTPTimeout = 30 * time.Second
)

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

type messagesRequest struct {
	Model     string                   `json:"model"`
	System    string                   `json:"system,omitempty"`
	Messages  []messagesRequestMessage `json:"messages"`
	MaxTokens int                      `json:"max_tokens"`
	Stream    bool                     `json:"stream,omitempty"`
}

type messagesRequestMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func New(cfg config.AnthropicConfig, client *http.Client) *Client {
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
	return "anthropic"
}

func (c *Client) Ready(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/models", nil)
	if err != nil {
		return fmt.Errorf("create readiness request: %w", err)
	}
	request.Header.Set("x-api-key", c.apiKey)
	request.Header.Set("anthropic-version", apiVersion)
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
		return fmt.Errorf("anthropic readiness returned status %d", response.StatusCode)
	}
	return nil
}

func (c *Client) ChatCompletions(ctx context.Context, req chat.ProviderRequest) (*http.Response, error) {
	translated, err := translateChatCompletionRequest(req.Request)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(translated)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create upstream request: %w", err)
	}

	request.Header.Set("x-api-key", c.apiKey)
	request.Header.Set("anthropic-version", apiVersion)
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

func translateChatCompletionRequest(req chat.Request) (messagesRequest, error) {
	translated := messagesRequest{
		Model:     req.Model,
		Messages:  make([]messagesRequestMessage, 0, len(req.Messages)),
		MaxTokens: defaultMaxTokens,
		Stream:    req.Stream,
	}
	systemParts := make([]string, 0, len(req.Messages))

	for idx, message := range req.Messages {
		content, err := chat.MessageText(message)
		if err != nil {
			return messagesRequest{}, fmt.Errorf("read messages[%d].content: %w", idx, err)
		}

		switch message.Role {
		case "system", "developer":
			systemParts = append(systemParts, content)
		case "user", "assistant":
			translated.Messages = append(translated.Messages, messagesRequestMessage{
				Role:    message.Role,
				Content: content,
			})
		default:
			return messagesRequest{}, fmt.Errorf("messages[%d].role %q is unsupported for anthropic", idx, message.Role)
		}
	}

	if len(systemParts) > 0 {
		translated.System = strings.Join(systemParts, "\n\n")
	}
	if len(translated.Messages) == 0 {
		return messagesRequest{}, fmt.Errorf("anthropic requests must include at least one user or assistant message")
	}

	return translated, nil
}
