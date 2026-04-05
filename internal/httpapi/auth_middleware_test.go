package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rillanai/rillan/internal/config"
	"github.com/rillanai/rillan/internal/observability"
)

func TestWrapProtectedEndpointBypassesAuthWhenDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Auth.Enabled = false
	handler := WrapProtectedEndpoint(nil, cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusNoContent; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestWrapProtectedEndpointRejectsMissingBearer(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Auth.Enabled = true
	daemonAuthBearerResolver = func(config.Config) (string, error) { return "secret", nil }
	t.Cleanup(func() { daemonAuthBearerResolver = config.ResolveServerAuthBearer })

	handler := WrapProtectedEndpoint(nil, cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusUnauthorized; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if got, want := recorder.Header().Get("WWW-Authenticate"), `Bearer realm="rillan"`; got != want {
		t.Fatalf("WWW-Authenticate = %q, want %q", got, want)
	}
}

func TestWrapProtectedEndpointRejectsWrongBearer(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Auth.Enabled = true
	daemonAuthBearerResolver = func(config.Config) (string, error) { return "secret", nil }
	t.Cleanup(func() { daemonAuthBearerResolver = config.ResolveServerAuthBearer })

	handler := WrapProtectedEndpoint(nil, cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	request.Header.Set("Authorization", "Bearer wrong")
	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusUnauthorized; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestWrapProtectedEndpointAllowsCorrectBearer(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Auth.Enabled = true
	daemonAuthBearerResolver = func(config.Config) (string, error) { return "secret", nil }
	t.Cleanup(func() { daemonAuthBearerResolver = config.ResolveServerAuthBearer })

	handler := WrapProtectedEndpoint(nil, cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := RequestIDFromContext(r.Context()); got == "" {
			t.Fatal("expected request id in context")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	request = request.WithContext(observability.WithRequestID(request.Context(), "req-123"))
	request.Header.Set("Authorization", "Bearer secret")
	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusNoContent; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestWrapProtectedEndpointReturnsConfigErrorWhenBearerResolutionFails(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Auth.Enabled = true
	daemonAuthBearerResolver = func(config.Config) (string, error) { return "", errors.New("missing secret") }
	t.Cleanup(func() { daemonAuthBearerResolver = config.ResolveServerAuthBearer })

	handler := WrapProtectedEndpoint(nil, cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	request.Header.Set("Authorization", "Bearer secret")
	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusInternalServerError; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if !strings.Contains(recorder.Body.String(), "server auth is misconfigured") {
		t.Fatalf("body = %s, want config error", recorder.Body.String())
	}
}
