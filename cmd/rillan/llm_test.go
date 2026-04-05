package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rillanai/rillan/internal/config"
	"github.com/rillanai/rillan/internal/secretstore"
)

func TestLLMAddCreatesProviderEntry(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	cmd := newLLMCommand()
	cmd.SetArgs([]string{"--config", configPath, "add", "work-gpt", "--backend", "openai_compatible", "--transport", "http", "--endpoint", "https://api.openai.com/v1", "--default-model", "gpt-5", "--capability", "chat", "--capability", "reasoning"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	cfg, err := config.LoadForEdit(configPath)
	if err != nil {
		t.Fatalf("LoadForEdit returned error: %v", err)
	}
	if got, want := cfg.LLMs.Default, "openai"; got != want {
		t.Fatalf("llms.default = %q, want %q", got, want)
	}
	if got, want := len(cfg.LLMs.Providers), 7; got != want {
		t.Fatalf("len(llms.providers) = %d, want %d", got, want)
	}
	provider := cfg.LLMs.Providers[6]
	if got, want := provider.AuthStrategy, config.AuthStrategyAPIKey; got != want {
		t.Fatalf("auth_strategy = %q, want %q", got, want)
	}
	if got, want := provider.Backend, config.LLMBackendOpenAICompatible; got != want {
		t.Fatalf("backend = %q, want %q", got, want)
	}
	if got, want := strings.Join(provider.Capabilities, ","), "chat,reasoning"; got != want {
		t.Fatalf("capabilities = %q, want %q", got, want)
	}
}

func TestLLMAddWithPresetCreatesBundledProviderEntry(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	cmd := newLLMCommand()
	cmd.SetArgs([]string{"--config", configPath, "add", "deepseek-team", "--preset", "deepseek"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	cfg, err := config.LoadForEdit(configPath)
	if err != nil {
		t.Fatalf("LoadForEdit returned error: %v", err)
	}
	provider := cfg.LLMs.Providers[len(cfg.LLMs.Providers)-1]
	if got, want := provider.Preset, config.LLMPresetDeepSeek; got != want {
		t.Fatalf("preset = %q, want %q", got, want)
	}
	if got, want := provider.Backend, config.LLMBackendOpenAICompatible; got != want {
		t.Fatalf("backend = %q, want %q", got, want)
	}
	if got, want := provider.Endpoint, "https://api.deepseek.com/v1"; got != want {
		t.Fatalf("endpoint = %q, want %q", got, want)
	}
	if got, want := provider.AuthStrategy, config.AuthStrategyAPIKey; got != want {
		t.Fatalf("auth_strategy = %q, want %q", got, want)
	}
}

func TestLLMUseSwitchesDefaultProvider(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.DefaultConfig()
	cfg.LLMs.Providers = []config.LLMProviderConfig{
		{ID: "work-gpt", Backend: config.LLMBackendOpenAICompatible, Transport: config.LLMTransportHTTP, Endpoint: "https://api.openai.com/v1", AuthStrategy: config.AuthStrategyBrowserOIDC},
		{ID: "repo-plugin", Backend: "custom-backend", Transport: config.LLMTransportSTDIO, Command: []string{"rillan-provider-demo"}, AuthStrategy: config.AuthStrategyNone},
	}
	if err := config.Write(configPath, cfg); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	cmd := newLLMCommand()
	cmd.SetArgs([]string{"--config", configPath, "use", "repo-plugin"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	reloaded, err := config.LoadForEdit(configPath)
	if err != nil {
		t.Fatalf("LoadForEdit returned error: %v", err)
	}
	if got, want := reloaded.LLMs.Default, "repo-plugin"; got != want {
		t.Fatalf("llms.default = %q, want %q", got, want)
	}
}

func TestLLMRemoveDeletesProviderEntry(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.DefaultConfig()
	cfg.LLMs.Default = "work-gpt"
	cfg.LLMs.Providers = []config.LLMProviderConfig{{ID: "work-gpt", Backend: config.LLMBackendOpenAICompatible, Transport: config.LLMTransportHTTP, Endpoint: "https://api.openai.com/v1", AuthStrategy: config.AuthStrategyBrowserOIDC}}
	if err := config.Write(configPath, cfg); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	cmd := newLLMCommand()
	cmd.SetArgs([]string{"--config", configPath, "remove", "work-gpt"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	reloaded, err := config.LoadForEdit(configPath)
	if err != nil {
		t.Fatalf("LoadForEdit returned error: %v", err)
	}
	if got := len(reloaded.LLMs.Providers); got != 6 {
		t.Fatalf("len(llms.providers) = %d, want 6", got)
	}
	if got, want := reloaded.LLMs.Default, "openai"; got != want {
		t.Fatalf("llms.default = %q, want %q", got, want)
	}
}

func TestLLMListPrintsSortedProviders(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.DefaultConfig()
	cfg.LLMs.Default = "work-gpt"
	cfg.LLMs.Providers = []config.LLMProviderConfig{
		{ID: "repo-plugin", Backend: "custom-backend", Transport: config.LLMTransportSTDIO, Command: []string{"rillan-provider-demo"}, AuthStrategy: config.AuthStrategyNone},
		{ID: "work-gpt", Preset: config.LLMPresetXAI, Backend: config.LLMBackendOpenAICompatible, Transport: config.LLMTransportHTTP, Endpoint: "https://api.x.ai/v1", AuthStrategy: config.AuthStrategyAPIKey},
	}
	if err := config.Write(configPath, cfg); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	cmd := newLLMCommand()
	cmd.SetArgs([]string{"--config", configPath, "list"})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "default: work-gpt") {
		t.Fatalf("output missing default:\n%s", output)
	}
	if !strings.Contains(output, "preset: xai") {
		t.Fatalf("output missing preset:\n%s", output)
	}
	if strings.Index(output, "- id: repo-plugin") > strings.Index(output, "- id: work-gpt") {
		t.Fatalf("providers not sorted by id:\n%s", output)
	}
	if !strings.Contains(output, "transport: stdio") {
		t.Fatalf("providers not sorted by id:\n%s", output)
	}
}

func TestLLMLoginAndLogoutStoreCredentialsSecurely(t *testing.T) {
	store := map[string]string{}
	secretstoreTestHooks(t, store)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.DefaultConfig()
	cfg.LLMs.Providers = []config.LLMProviderConfig{{ID: "work-gpt", Backend: config.LLMBackendOpenAICompatible, Transport: config.LLMTransportHTTP, Endpoint: "https://api.openai.com/v1", AuthStrategy: config.AuthStrategyBrowserOIDC, CredentialRef: credentialRefForLLM("work-gpt")}}
	if err := config.Write(configPath, cfg); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	login := newLLMCommand()
	login.SetArgs([]string{"--config", configPath, "login", "work-gpt", "--access-token", "token-1", "--issuer", "issuer-a"})
	if err := login.Execute(); err != nil {
		t.Fatalf("login Execute returned error: %v", err)
	}
	if _, ok := store[fmt.Sprintf("rillan/llm/%s", "work-gpt")]; !ok {
		t.Fatal("expected keyring entry to be created")
	}
	if !secretstore.Exists(credentialRefForLLM("work-gpt")) {
		t.Fatal("expected credential ref to resolve")
	}

	logout := newLLMCommand()
	logout.SetArgs([]string{"--config", configPath, "logout", "work-gpt"})
	if err := logout.Execute(); err != nil {
		t.Fatalf("logout Execute returned error: %v", err)
	}
	if secretstore.Exists(credentialRefForLLM("work-gpt")) {
		t.Fatal("expected credential ref to be cleared")
	}
}

func TestLLMUseNotifiesDaemonRefresh(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.DefaultConfig()
	cfg.LLMs.Providers = []config.LLMProviderConfig{{ID: "work-gpt", Backend: config.LLMBackendOpenAICompatible, Transport: config.LLMTransportHTTP, Endpoint: "https://api.openai.com/v1", AuthStrategy: config.AuthStrategyAPIKey}}
	if err := config.Write(configPath, cfg); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	called := 0
	daemonRefreshNotifier = func(config.Config) error {
		called++
		return nil
	}
	t.Cleanup(func() { daemonRefreshNotifier = notifyDaemonRuntimeRefresh })

	cmd := newLLMCommand()
	cmd.SetArgs([]string{"--config", configPath, "use", "work-gpt"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if got, want := called, 1; got != want {
		t.Fatalf("daemon refresh calls = %d, want %d", got, want)
	}
}
