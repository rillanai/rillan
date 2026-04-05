package stdio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"

	"github.com/rillanai/rillan/internal/chat"
)

type Client struct {
	command []string
}

type chatCompletionRequestEnvelope struct {
	Request chat.Request    `json:"request"`
	RawBody json.RawMessage `json:"raw_body"`
}

type chatCompletionResponseEnvelope struct {
	StatusCode int                 `json:"status_code"`
	Headers    map[string][]string `json:"headers"`
	Body       json.RawMessage     `json:"body"`
}

func New(command []string) *Client {
	return &Client{command: append([]string(nil), command...)}
}

func (c *Client) Name() string {
	return "stdio"
}

func (c *Client) Ready(context.Context) error {
	if len(c.command) == 0 {
		return fmt.Errorf("stdio provider command must not be empty")
	}
	if _, err := exec.LookPath(c.command[0]); err != nil {
		return fmt.Errorf("resolve stdio provider command: %w", err)
	}
	return nil
}

func (c *Client) ChatCompletions(ctx context.Context, request chat.ProviderRequest) (*http.Response, error) {
	if request.Request.Stream {
		return nil, fmt.Errorf("stdio provider does not support streaming responses")
	}
	if err := c.Ready(ctx); err != nil {
		return nil, err
	}

	payload, err := json.Marshal(chatCompletionRequestEnvelope{Request: request.Request, RawBody: json.RawMessage(request.RawBody)})
	if err != nil {
		return nil, fmt.Errorf("marshal stdio provider request: %w", err)
	}

	cmd := exec.CommandContext(ctx, c.command[0], c.command[1:]...)
	cmd.Stdin = bytes.NewReader(payload)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderrText := strings.TrimSpace(stderr.String())
		if stderrText != "" {
			return nil, fmt.Errorf("run stdio provider: %w: %s", err, stderrText)
		}
		return nil, fmt.Errorf("run stdio provider: %w", err)
	}
	if stdout.Len() == 0 {
		return nil, fmt.Errorf("read stdio provider response: empty stdout")
	}

	var responseEnvelope chatCompletionResponseEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &responseEnvelope); err != nil {
		return nil, fmt.Errorf("decode stdio provider response: %w", err)
	}

	statusCode := responseEnvelope.StatusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	if statusCode < 100 || statusCode > 999 {
		return nil, fmt.Errorf("decode stdio provider response: invalid status_code %d", statusCode)
	}
	headers := make(http.Header, len(responseEnvelope.Headers))
	for key, values := range responseEnvelope.Headers {
		headers[key] = append([]string(nil), values...)
	}
	if headers.Get("Content-Type") == "" {
		headers.Set("Content-Type", "application/json")
	}

	return &http.Response{
		StatusCode: statusCode,
		Header:     headers,
		Body:       io.NopCloser(bytes.NewReader(responseEnvelope.Body)),
	}, nil
}
