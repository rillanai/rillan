package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"

	internalopenai "github.com/sidekickos/rillan/internal/openai"
	"github.com/sidekickos/rillan/internal/providers"
)

type ChatCompletionsHandler struct {
	logger   *slog.Logger
	provider providers.Provider
}

func NewChatCompletionsHandler(logger *slog.Logger, provider providers.Provider) *ChatCompletionsHandler {
	if logger == nil {
		logger = slog.Default()
	}

	return &ChatCompletionsHandler{
		logger:   logger,
		provider: provider,
	}
}

func (h *ChatCompletionsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		internalopenai.WriteError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method must be POST")
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
	if err != nil {
		internalopenai.WriteError(w, http.StatusBadRequest, "invalid_request_error", "request body could not be read")
		return
	}

	var request internalopenai.ChatCompletionRequest
	if err := json.Unmarshal(body, &request); err != nil {
		internalopenai.WriteError(w, http.StatusBadRequest, "invalid_request_error", "request body must be valid JSON")
		return
	}

	if err := internalopenai.ValidateChatCompletionRequest(request); err != nil {
		internalopenai.WriteError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	response, err := h.provider.ChatCompletions(r.Context(), request, body)
	if err != nil {
		h.logger.Error("upstream request failed", "request_id", RequestIDFromContext(r.Context()), "provider", h.provider.Name(), "error", err.Error())
		status := http.StatusBadGateway
		if isTimeout(err) {
			status = http.StatusGatewayTimeout
		}
		internalopenai.WriteError(w, status, "upstream_error", "upstream request failed")
		return
	}
	defer response.Body.Close()

	copyHeaders(w.Header(), response.Header)
	w.WriteHeader(response.StatusCode)

	if err := copyBody(w, response.Body, request.Stream || strings.Contains(response.Header.Get("Content-Type"), "text/event-stream")); err != nil {
		h.logger.Error("proxy response copy failed", "request_id", RequestIDFromContext(r.Context()), "provider", h.provider.Name(), "error", err.Error())
	}
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func copyHeaders(target, source http.Header) {
	for key, values := range source {
		if isHopByHopHeader(key) {
			continue
		}
		for _, value := range values {
			target.Add(key, value)
		}
	}
}

func isHopByHopHeader(key string) bool {
	switch http.CanonicalHeaderKey(key) {
	case "Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade":
		return true
	default:
		return false
	}
}

func copyBody(w http.ResponseWriter, body io.Reader, streaming bool) error {
	if !streaming {
		_, err := io.Copy(w, body)
		return err
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming response writer does not implement http.Flusher")
	}

	buffer := make([]byte, 32*1024)
	for {
		n, err := body.Read(buffer)
		if n > 0 {
			if _, writeErr := w.Write(buffer[:n]); writeErr != nil {
				return writeErr
			}
			flusher.Flush()
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
