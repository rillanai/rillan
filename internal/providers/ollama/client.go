package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sidekickos/rillan/internal/config"
	internalollama "github.com/sidekickos/rillan/internal/ollama"
	internalopenai "github.com/sidekickos/rillan/internal/openai"
)

type Client struct {
	client *internalollama.Client
}

type chatCompletionResponse struct {
	ID      string                   `json:"id"`
	Object  string                   `json:"object"`
	Created int64                    `json:"created"`
	Model   string                   `json:"model"`
	Choices []chatCompletionChoice   `json:"choices"`
	Usage   chatCompletionUsageStats `json:"usage"`
}

type chatCompletionChoice struct {
	Index        int                 `json:"index"`
	Message      chatCompletionReply `json:"message"`
	FinishReason string              `json:"finish_reason"`
}

type chatCompletionReply struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionUsageStats struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func New(cfg config.LocalModelProvider, httpClient *http.Client) *Client {
	return &Client{client: internalollama.New(cfg.BaseURL, httpClient)}
}

func (c *Client) Name() string {
	return "ollama"
}

func (c *Client) Ready(ctx context.Context) error {
	return c.client.Ping(ctx)
}

func (c *Client) ChatCompletions(ctx context.Context, req internalopenai.ChatCompletionRequest, _ []byte) (*http.Response, error) {
	prompt, err := buildPrompt(req)
	if err != nil {
		return nil, err
	}

	content, err := c.client.Generate(ctx, req.Model, prompt)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(chatCompletionResponse{
		ID:      "chatcmpl-ollama",
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []chatCompletionChoice{{
			Index: 0,
			Message: chatCompletionReply{
				Role:    "assistant",
				Content: content,
			},
			FinishReason: "stop",
		}},
		Usage: chatCompletionUsageStats{},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal ollama chat completion response: %w", err)
	}

	header := make(http.Header)
	header.Set("Content-Type", "application/json")

	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     header,
		Body:       io.NopCloser(bytes.NewReader(body)),
	}, nil
}

func buildPrompt(req internalopenai.ChatCompletionRequest) (string, error) {
	parts := make([]string, 0, len(req.Messages)+1)
	for idx, message := range req.Messages {
		content, err := internalopenai.MessageText(message)
		if err != nil {
			return "", fmt.Errorf("read messages[%d].content: %w", idx, err)
		}
		parts = append(parts, fmt.Sprintf("%s: %s", message.Role, content))
	}
	parts = append(parts, "assistant:")
	return strings.Join(parts, "\n\n"), nil
}
