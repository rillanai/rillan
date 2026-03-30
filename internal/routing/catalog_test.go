package routing

import (
	"testing"

	"github.com/sidekickos/rillan/internal/config"
)

func TestBuildCatalogDerivesExecutionLocationFromFamilyAndTransport(t *testing.T) {
	t.Parallel()

	catalog := BuildCatalog(config.Config{
		SchemaVersion: config.SchemaVersionV2,
		LLMs: config.LLMRegistryConfig{
			Providers: []config.LLMProviderConfig{
				{ID: "local-chat", Backend: config.ProviderOllama, Transport: config.LLMTransportHTTP, DefaultModel: "qwen3:8b"},
				{ID: "openai", Backend: config.ProviderOpenAICompatible, Transport: config.LLMTransportHTTP, DefaultModel: "gpt-5", Capabilities: []string{"chat"}},
				{ID: "stdio-local", Backend: config.ProviderOpenAICompatible, Transport: config.LLMTransportSTDIO, Command: []string{"demo-provider"}, DefaultModel: "demo-model"},
			},
		},
	}, config.DefaultProjectConfig())

	if got, want := len(catalog.Candidates), 3; got != want {
		t.Fatalf("candidate count = %d, want %d", got, want)
	}
	if got, want := catalog.Candidates[0].ID, "local-chat"; got != want {
		t.Fatalf("candidates[0].id = %q, want %q", got, want)
	}
	if got, want := catalog.Candidates[0].Location, LocationLocal; got != want {
		t.Fatalf("local-chat location = %q, want %q", got, want)
	}
	if got, want := catalog.Candidates[1].Location, LocationRemote; got != want {
		t.Fatalf("openai location = %q, want %q", got, want)
	}
	if got, want := catalog.Candidates[2].Location, LocationLocal; got != want {
		t.Fatalf("stdio-local location = %q, want %q", got, want)
	}
}

func TestBuildCatalogAppliesProjectAllowlist(t *testing.T) {
	t.Parallel()

	catalog := BuildCatalog(config.Config{
		SchemaVersion: config.SchemaVersionV2,
		LLMs: config.LLMRegistryConfig{
			Providers: []config.LLMProviderConfig{
				{ID: "alpha", Backend: config.ProviderOpenAICompatible, Transport: config.LLMTransportHTTP},
				{ID: "beta", Backend: config.ProviderOllama, Transport: config.LLMTransportHTTP},
				{ID: "gamma", Backend: config.ProviderAnthropic, Transport: config.LLMTransportHTTP},
			},
		},
	}, config.ProjectConfig{
		Providers: config.ProjectProviderSelectionConfig{LLMAllowed: []string{"beta", "alpha"}},
	})

	if got, want := len(catalog.Candidates), 2; got != want {
		t.Fatalf("candidate count = %d, want %d", got, want)
	}
	if got, want := catalog.Candidates[0].ID, "alpha"; got != want {
		t.Fatalf("candidates[0].id = %q, want %q", got, want)
	}
	if got, want := catalog.Candidates[1].ID, "beta"; got != want {
		t.Fatalf("candidates[1].id = %q, want %q", got, want)
	}
	if got := catalog.ByID["gamma"]; got.ID != "" {
		t.Fatalf("expected gamma to be excluded, got %+v", got)
	}
	if got, want := catalog.Allowed, true; got != want {
		t.Fatalf("catalog allowed = %t, want %t", got, want)
	}
}
