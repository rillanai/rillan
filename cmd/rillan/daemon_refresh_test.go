// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/rillanai/rillan/internal/config"
	"github.com/rillanai/rillan/internal/httpapi"
)

func TestNotifyDaemonRuntimeRefreshIgnoresConnectionRefused(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = port

	if err := notifyDaemonRuntimeRefresh(cfg); err != nil {
		t.Fatalf("notifyDaemonRuntimeRefresh returned error: %v", err)
	}
}

func TestNotifyDaemonRuntimeRefreshSurfacesDaemonFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != httpapi.AdminRuntimeRefreshPath {
			t.Fatalf("path = %q, want %q", r.URL.Path, httpapi.AdminRuntimeRefreshPath)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"reload failed"}}`))
	}))
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse returned error: %v", err)
	}
	host, portText, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatalf("SplitHostPort returned error: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("Atoi returned error: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Server.Host = host
	cfg.Server.Port = port

	err = notifyDaemonRuntimeRefresh(cfg)
	if err == nil {
		t.Fatal("expected refresh error")
	}
	if got := err.Error(); got == "" || !strings.Contains(got, "reload failed") {
		t.Fatalf("error = %q, want reload failed", got)
	}
}

func TestNotifyDaemonRuntimeRefreshIncludesAuthorizationWhenServerAuthEnabled(t *testing.T) {
	t.Cleanup(resetSecretstoreTestHooks)
	secretstoreKeyringGet(func(service string, user string) (string, error) {
		if got, want := fmt.Sprintf("%s/%s", service, user), "rillan/auth/daemon"; got != want {
			return "", fmt.Errorf("keyring lookup = %q, want %q", got, want)
		}
		return `{"api_key":"daemon-secret","auth_strategy":"api_key"}`, nil
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Authorization"), "Bearer daemon-secret"; got != want {
			t.Fatalf("Authorization = %q, want %q", got, want)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse returned error: %v", err)
	}
	host, portText, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatalf("SplitHostPort returned error: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("Atoi returned error: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Server.Host = host
	cfg.Server.Port = port
	cfg.Server.Auth.Enabled = true

	if err := notifyDaemonRuntimeRefresh(cfg); err != nil {
		t.Fatalf("notifyDaemonRuntimeRefresh returned error: %v", err)
	}
}

func TestRefreshDaemonAfterMutationAppliesEnvironmentAuthOverrides(t *testing.T) {
	t.Cleanup(resetSecretstoreTestHooks)
	t.Setenv("RILLAN_SERVER_AUTH_ENABLED", "true")
	secretstoreKeyringGet(func(service string, user string) (string, error) {
		if got, want := fmt.Sprintf("%s/%s", service, user), "rillan/auth/daemon"; got != want {
			return "", fmt.Errorf("keyring lookup = %q, want %q", got, want)
		}
		return `{"api_key":"daemon-secret","auth_strategy":"api_key"}`, nil
	})

	original := daemonRefreshNotifier
	t.Cleanup(func() { daemonRefreshNotifier = original })
	daemonRefreshNotifier = func(cfg config.Config) error {
		if !cfg.Server.Auth.Enabled {
			t.Fatal("expected env override to enable server auth")
		}
		return nil
	}

	cfg := config.DefaultConfig()
	cfg.Server.Auth.Enabled = false

	if err := refreshDaemonAfterMutation(cfg, "updated control-plane auth"); err != nil {
		t.Fatalf("refreshDaemonAfterMutation returned error: %v", err)
	}
}
