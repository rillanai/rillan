// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rillanai/rillan/internal/chat"
	"github.com/rillanai/rillan/internal/config"
	"github.com/rillanai/rillan/internal/observability"
	"github.com/rillanai/rillan/internal/providers"
)

type routerTestProvider struct{}

func (routerTestProvider) Name() string { return "test" }

func (routerTestProvider) Ready(context.Context) error { return nil }

func (routerTestProvider) ChatCompletions(context.Context, chat.ProviderRequest) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header)}, nil
}

var _ providers.Provider = routerTestProvider{}

func TestNewRouterOmitsAgentRoutesWhenDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agent.Enabled = false
	handler := NewRouter(nil, routerTestProvider{}, cfg, RouterOptions{})

	for _, path := range []string{"/v1/agent/tasks", "/v1/agent/proposals/example/decision"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, path, nil)
		handler.ServeHTTP(recorder, request)

		if got, want := recorder.Code, http.StatusNotFound; got != want {
			t.Fatalf("%s status = %d, want %d", path, got, want)
		}
	}
}

func TestNewRouterRegistersAgentRoutesWhenEnabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agent.Enabled = true
	cfg.Server.Auth.Enabled = true
	daemonAuthBearerResolver = func(config.Config) (string, error) { return "secret", nil }
	t.Cleanup(func() { daemonAuthBearerResolver = config.ResolveServerAuthBearer })
	handler := NewRouter(nil, routerTestProvider{}, cfg, RouterOptions{})

	tests := []struct {
		name string
		path string
		body string
	}{
		{name: "tasks", path: "/v1/agent/tasks", body: `{`},
		{name: "proposals", path: "/v1/agent/proposals/example/decision", body: `{`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			request.Header.Set("Authorization", "Bearer secret")
			handler.ServeHTTP(recorder, request)

			if got, want := recorder.Code, http.StatusBadRequest; got != want {
				t.Fatalf("%s status = %d, want %d", tt.path, got, want)
			}
		})
	}
}

func TestNewRouterLeavesHealthEndpointsOpenWhenServerAuthEnabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Auth.Enabled = true
	daemonAuthBearerResolver = func(config.Config) (string, error) { return "secret", nil }
	t.Cleanup(func() { daemonAuthBearerResolver = config.ResolveServerAuthBearer })
	handler := NewRouter(nil, routerTestProvider{}, cfg, RouterOptions{})

	for _, path := range []string{"/healthz", "/readyz"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		handler.ServeHTTP(recorder, request)

		if got, want := recorder.Code, http.StatusOK; got != want {
			t.Fatalf("%s status = %d, want %d", path, got, want)
		}
	}
}

func TestNewRouterProtectsChatEndpointWhenServerAuthEnabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Auth.Enabled = true
	daemonAuthBearerResolver = func(config.Config) (string, error) { return "secret", nil }
	t.Cleanup(func() { daemonAuthBearerResolver = config.ResolveServerAuthBearer })
	handler := NewRouter(nil, routerTestProvider{}, cfg, RouterOptions{})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5"}`))
	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusUnauthorized; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestNewRouterProtectsAdminRefreshWhenServerAuthEnabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Auth.Enabled = true
	daemonAuthBearerResolver = func(config.Config) (string, error) { return "secret", nil }
	t.Cleanup(func() { daemonAuthBearerResolver = config.ResolveServerAuthBearer })
	handler := NewRouter(nil, routerTestProvider{}, cfg, RouterOptions{RefreshRuntime: func(context.Context) error { return nil }})

	unauthorizedRecorder := httptest.NewRecorder()
	unauthorizedRequest := httptest.NewRequest(http.MethodPost, AdminRuntimeRefreshPath, nil)
	unauthorizedRequest.RemoteAddr = "127.0.0.1:12345"
	handler.ServeHTTP(unauthorizedRecorder, unauthorizedRequest)

	if got, want := unauthorizedRecorder.Code, http.StatusUnauthorized; got != want {
		t.Fatalf("unauthorized status = %d, want %d", got, want)
	}

	authorizedRecorder := httptest.NewRecorder()
	authorizedRequest := httptest.NewRequest(http.MethodPost, AdminRuntimeRefreshPath, nil)
	authorizedRequest.RemoteAddr = "127.0.0.1:12345"
	authorizedRequest.Header.Set("Authorization", "Bearer secret")
	handler.ServeHTTP(authorizedRecorder, authorizedRequest)

	if got, want := authorizedRecorder.Code, http.StatusNoContent; got != want {
		t.Fatalf("authorized status = %d, want %d", got, want)
	}
}

func TestNewRouterExposesProtectedMetricsEndpoint(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Auth.Enabled = true
	daemonAuthBearerResolver = func(config.Config) (string, error) { return "secret", nil }
	t.Cleanup(func() { daemonAuthBearerResolver = config.ResolveServerAuthBearer })
	metrics := observability.NewRegistry()
	handler := NewRouter(nil, routerTestProvider{}, cfg, RouterOptions{Metrics: metrics})

	unauthorizedRecorder := httptest.NewRecorder()
	unauthorizedRequest := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	handler.ServeHTTP(unauthorizedRecorder, unauthorizedRequest)
	if got, want := unauthorizedRecorder.Code, http.StatusUnauthorized; got != want {
		t.Fatalf("unauthorized status = %d, want %d", got, want)
	}

	authorizedRecorder := httptest.NewRecorder()
	authorizedRequest := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	authorizedRequest.Header.Set("Authorization", "Bearer secret")
	handler.ServeHTTP(authorizedRecorder, authorizedRequest)
	if got, want := authorizedRecorder.Code, http.StatusOK; got != want {
		t.Fatalf("authorized status = %d, want %d", got, want)
	}
	if !strings.Contains(authorizedRecorder.Body.String(), "rillan_http_requests_total") {
		t.Fatalf("metrics body = %s, want request metric", authorizedRecorder.Body.String())
	}
}
