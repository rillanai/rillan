package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rillanai/rillan/internal/observability"
)

func TestWrapWithMiddlewareAddsRequestIDHeader(t *testing.T) {
	handler := WrapWithMiddleware(slog.New(slog.NewTextHandler(io.Discard, nil)), observability.NewRegistry(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RequestIDFromContext(r.Context()) == "" {
			t.Fatal("missing request id in context")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Header().Get("X-Request-ID") == "" {
		t.Fatal("missing X-Request-ID header")
	}
}

func TestRequestLoggerDoesNotLeakAuthorizationHeader(t *testing.T) {
	buffer := &strings.Builder{}
	logger := slog.New(slog.NewTextHandler(buffer, nil))
	handler := WrapWithMiddleware(logger, observability.NewRegistry(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request = request.WithContext(context.Background())
	request.Header.Set("Authorization", "Bearer secret")
	handler.ServeHTTP(recorder, request)

	if strings.Contains(buffer.String(), "secret") {
		t.Fatalf("log output leaked authorization header: %s", buffer.String())
	}
}
