package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/sidekickos/rillan/internal/config"
	internalopenai "github.com/sidekickos/rillan/internal/openai"
)

const (
	apiVersion       = "2023-06-01"
	defaultMaxTokens = 1024
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
		client = http.DefaultClient
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

func (c *Client) Ready(context.Context) error {
	return nil
}

func (c *Client) ChatCompletions(ctx context.Context, req internalopenai.ChatCompletionRequest, _ []byte) (*http.Response, error) {
	translated, err := translateChatCompletionRequest(req)
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

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("perform upstream request: %w", err)
	}

	return response, nil
}

func translateChatCompletionRequest(req internalopenai.ChatCompletionRequest) (messagesRequest, error) {
	translated := messagesRequest{
		Model:     req.Model,
		Messages:  make([]messagesRequestMessage, 0, len(req.Messages)),
		MaxTokens: defaultMaxTokens,
		Stream:    req.Stream,
	}
	systemParts := make([]string, 0, len(req.Messages))

	for idx, message := range req.Messages {
		content, err := internalopenai.MessageText(message)
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
