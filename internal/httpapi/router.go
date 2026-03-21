package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/sidekickos/rillan/internal/providers"
)

func NewRouter(logger *slog.Logger, provider providers.Provider) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", HealthHandler)
	mux.HandleFunc("GET /readyz", ReadyHandler(provider))
	mux.Handle("/v1/chat/completions", NewChatCompletionsHandler(logger, provider))

	return WrapWithMiddleware(logger, mux)
}
