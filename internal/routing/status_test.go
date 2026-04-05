package routing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/rillanai/rillan/internal/config"
	"github.com/rillanai/rillan/internal/secretstore"
	keyring "github.com/zalando/go-keyring"
)

func TestBuildStatusCatalogMarksConfiguredRemoteProviderUnauthenticated(t *testing.T) {
	secretstore.SetKeyringGetForTest(func(service string, user string) (string, error) {
		return "", keyring.ErrNotFound
	})
	t.Cleanup(func() {
		secretstore.SetKeyringGetForTest(keyring.Get)
	})

	cfg := config.Config{
		SchemaVersion: config.SchemaVersionV2,
		LLMs: config.LLMRegistryConfig{
			Providers: []config.LLMProviderConfig{{
				ID:            "remote-gpt",
				Backend:       config.ProviderOpenAICompatible,
				Transport:     config.LLMTransportHTTP,
				Endpoint:      "https://api.openai.com/v1",
				AuthStrategy:  config.AuthStrategyAPIKey,
				CredentialRef: "keyring://rillan/llm/remote-gpt",
			}},
		},
	}

	statuses := BuildStatusCatalog(context.Background(), StatusInput{
		Catalog: BuildCatalog(cfg, config.DefaultProjectConfig()),
		Config:  cfg,
	})

	if got, want := len(statuses.Candidates), 1; got != want {
		t.Fatalf("candidate count = %d, want %d", got, want)
	}

	status := statuses.Candidates[0]
	if !status.Configured {
		t.Fatal("expected configured candidate")
	}
	if status.AuthValid {
		t.Fatal("expected auth to be invalid")
	}
	if status.Ready {
		t.Fatal("expected candidate to be not ready when auth is invalid")
	}
	if status.Available {
		t.Fatal("expected candidate to be unavailable")
	}
	requireUnavailableReason(t, status, UnavailableReasonMissingCredentials)
}

func TestBuildStatusCatalogMarksAuthValidProviderNotReady(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cfg := config.Config{
		SchemaVersion: config.SchemaVersionV2,
		LLMs: config.LLMRegistryConfig{
			Providers: []config.LLMProviderConfig{{
				ID:        "local-chat",
				Backend:   config.ProviderOllama,
				Transport: config.LLMTransportHTTP,
				Endpoint:  server.URL,
			}},
		},
	}

	statuses := BuildStatusCatalog(context.Background(), StatusInput{
		Catalog:    BuildCatalog(cfg, config.DefaultProjectConfig()),
		Config:     cfg,
		HTTPClient: server.Client(),
	})

	status := statuses.ByID["local-chat"]
	if !status.Configured {
		t.Fatal("expected configured candidate")
	}
	if !status.AuthValid {
		t.Fatal("expected auth to be valid")
	}
	if status.Ready {
		t.Fatal("expected provider to be not ready")
	}
	if status.Available {
		t.Fatal("expected provider to be unavailable")
	}
	requireUnavailableReason(t, status, UnavailableReasonNotReady)
}

func TestBuildStatusCatalogMarksHealthyLocalProviderAvailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := config.Config{
		SchemaVersion: config.SchemaVersionV2,
		LLMs: config.LLMRegistryConfig{
			Providers: []config.LLMProviderConfig{{
				ID:        "local-chat",
				Backend:   config.ProviderOllama,
				Transport: config.LLMTransportHTTP,
				Endpoint:  server.URL,
			}},
		},
	}

	statuses := BuildStatusCatalog(context.Background(), StatusInput{
		Catalog:    BuildCatalog(cfg, config.DefaultProjectConfig()),
		Config:     cfg,
		HTTPClient: server.Client(),
	})

	status := statuses.ByID["local-chat"]
	if !status.Configured {
		t.Fatal("expected configured candidate")
	}
	if !status.AuthValid {
		t.Fatal("expected auth to be valid")
	}
	if !status.Ready {
		t.Fatal("expected provider to be ready")
	}
	if !status.Available {
		t.Fatal("expected provider to be available")
	}
	if got := len(status.UnavailableReasons); got != 0 {
		t.Fatalf("unavailable reason count = %d, want 0", got)
	}
}

func TestBuildStatusCatalogReturnsStableProviderIDOrdering(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := config.Config{
		SchemaVersion: config.SchemaVersionV2,
		LLMs: config.LLMRegistryConfig{
			Providers: []config.LLMProviderConfig{
				{ID: "zeta", Backend: config.ProviderOllama, Transport: config.LLMTransportHTTP, Endpoint: server.URL},
				{ID: "alpha", Backend: config.ProviderOllama, Transport: config.LLMTransportHTTP, Endpoint: server.URL},
				{ID: "beta", Backend: config.ProviderOllama, Transport: config.LLMTransportHTTP, Endpoint: server.URL},
			},
		},
	}

	catalog := BuildCatalog(cfg, config.DefaultProjectConfig())
	statuses := BuildStatusCatalog(context.Background(), StatusInput{
		Catalog:    Catalog{Candidates: []Candidate{catalog.Candidates[2], catalog.Candidates[0], catalog.Candidates[1]}},
		Config:     cfg,
		HTTPClient: server.Client(),
	})

	got := []string{statuses.Candidates[0].Candidate.ID, statuses.Candidates[1].Candidate.ID, statuses.Candidates[2].Candidate.ID}
	want := []string{"alpha", "beta", "zeta"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidates[%d] id = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildStatusCatalogMarksHealthyStdioProviderAvailable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is unix-specific")
	}

	script := writeExecutableStatusScript(t)
	cfg := config.Config{
		SchemaVersion: config.SchemaVersionV2,
		LLMs: config.LLMRegistryConfig{
			Providers: []config.LLMProviderConfig{{
				ID:           "stdio-demo",
				Backend:      config.ProviderOpenAICompatible,
				Transport:    config.LLMTransportSTDIO,
				Command:      []string{script},
				AuthStrategy: config.AuthStrategyNone,
			}},
		},
	}

	statuses := BuildStatusCatalog(context.Background(), StatusInput{
		Catalog: BuildCatalog(cfg, config.DefaultProjectConfig()),
		Config:  cfg,
	})

	status := statuses.ByID["stdio-demo"]
	if !status.Configured || !status.AuthValid || !status.Ready || !status.Available {
		t.Fatalf("expected stdio provider to be fully available, got %#v", status)
	}
}

func TestBuildStatusCatalogMarksMissingStdioProviderUnavailable(t *testing.T) {
	cfg := config.Config{
		SchemaVersion: config.SchemaVersionV2,
		LLMs: config.LLMRegistryConfig{
			Providers: []config.LLMProviderConfig{{
				ID:           "stdio-demo",
				Backend:      config.ProviderOpenAICompatible,
				Transport:    config.LLMTransportSTDIO,
				Command:      []string{"definitely-missing-rillan-stdio-provider"},
				AuthStrategy: config.AuthStrategyNone,
			}},
		},
	}

	statuses := BuildStatusCatalog(context.Background(), StatusInput{
		Catalog: BuildCatalog(cfg, config.DefaultProjectConfig()),
		Config:  cfg,
	})

	status := statuses.ByID["stdio-demo"]
	if !status.Configured || !status.AuthValid {
		t.Fatalf("expected stdio provider to be configured with valid auth, got %#v", status)
	}
	if status.Ready || status.Available {
		t.Fatalf("expected stdio provider to be unavailable, got %#v", status)
	}
	requireUnavailableReason(t, status, UnavailableReasonNotReady)
}

func writeExecutableStatusScript(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "provider.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	return path
}

func requireUnavailableReason(t *testing.T, status CandidateStatus, want UnavailableReasonCode) {
	t.Helper()

	for _, reason := range status.UnavailableReasons {
		if reason.Code == want {
			return
		}
	}

	t.Fatalf("unavailable reasons = %#v, want code %q", status.UnavailableReasons, want)
}
