// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	keyring "github.com/zalando/go-keyring"

	"github.com/rillanai/rillan/internal/config"
	"github.com/rillanai/rillan/internal/secretstore"
)

func TestNewRejectsEnabledServerAuthWithoutResolvableBearer(t *testing.T) {
	secretstore.SetKeyringGetForTest(func(service string, user string) (string, error) {
		return "", keyring.ErrNotFound
	})
	t.Cleanup(func() {
		secretstore.SetKeyringGetForTest(keyring.Get)
	})

	cfg := config.DefaultConfig()
	cfg.Server.Auth.Enabled = true

	_, err := New(cfg, config.DefaultProjectConfig(), nil, filepath.Join(t.TempDir(), "config.yaml"), filepath.Join(t.TempDir(), ".rillan", "project.yaml"), filepath.Join(t.TempDir(), "system.yaml"), nil)
	if err == nil {
		t.Fatal("expected New to fail when server auth bearer cannot be resolved")
	}
	if !errors.Is(err, keyring.ErrNotFound) {
		t.Fatalf("error = %v, want keyring errNotFound", err)
	}
}

func TestNewConfiguresServerTimeouts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := config.DefaultConfig()
	cfg.SchemaVersion = config.SchemaVersionV1
	cfg.LLMs = config.LLMRegistryConfig{}
	cfg.Provider.Type = config.ProviderOpenAI
	cfg.Provider.OpenAI.APIKey = "test-key"
	cfg.Provider.OpenAI.BaseURL = server.URL
	app, err := New(cfg, config.DefaultProjectConfig(), nil, filepath.Join(t.TempDir(), "config.yaml"), filepath.Join(t.TempDir(), ".rillan", "project.yaml"), filepath.Join(t.TempDir(), "system.yaml"), nil)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if got, want := app.server.ReadHeaderTimeout, serverReadHeaderTimeout; got != want {
		t.Fatalf("ReadHeaderTimeout = %v, want %v", got, want)
	}
	if got, want := app.server.ReadTimeout, serverReadTimeout; got != want {
		t.Fatalf("ReadTimeout = %v, want %v", got, want)
	}
	if got, want := app.server.WriteTimeout, serverWriteTimeout; got != want {
		t.Fatalf("WriteTimeout = %v, want %v", got, want)
	}
	if got, want := app.server.IdleTimeout, serverIdleTimeout; got != want {
		t.Fatalf("IdleTimeout = %v, want %v", got, want)
	}
}
