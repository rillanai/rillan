package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sidekickos/rillan/internal/secretstore"
	keyring "github.com/zalando/go-keyring"
)

func TestLoadAppliesEnvOverrides(t *testing.T) {
	t.Setenv("RILLAN_OPENAI_API_KEY", "env-openai-key")
	t.Setenv("RILLAN_SERVER_PORT", "9001")
	t.Setenv("RILLAN_INDEX_ROOT", "/tmp/project")

	path := writeTempConfig(t, `server:
  host: "127.0.0.1"
provider:
  type: "openai"
  openai:
    api_key: "file-openai-key"
runtime:
  vector_store_mode: "embedded"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if got, want := cfg.Provider.OpenAI.APIKey, "env-openai-key"; got != want {
		t.Fatalf("openai api key = %q, want %q", got, want)
	}

	if got, want := cfg.Server.Port, 9001; got != want {
		t.Fatalf("server port = %d, want %d", got, want)
	}

	if got, want := cfg.Index.Root, "/tmp/project"; got != want {
		t.Fatalf("index root = %q, want %q", got, want)
	}
}

func TestLoadDefaultsProviderTypeToOpenAI(t *testing.T) {
	t.Setenv("RILLAN_OPENAI_API_KEY", "test-key")

	path := writeTempConfig(t, `runtime:
  vector_store_mode: "embedded"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if got, want := cfg.Provider.Type, ProviderOpenAI; got != want {
		t.Fatalf("provider.type = %q, want %q", got, want)
	}
	if got, want := cfg.SchemaVersion, SchemaVersionV2; got != want {
		t.Fatalf("schema_version = %d, want %d", got, want)
	}
}

func TestLoadAppliesIndexDefaults(t *testing.T) {
	t.Setenv("RILLAN_OPENAI_API_KEY", "test-key")

	path := writeTempConfig(t, `runtime:
  vector_store_mode: "embedded"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if got, want := cfg.Index.ChunkSizeLines, 120; got != want {
		t.Fatalf("chunk_size_lines = %d, want %d", got, want)
	}
	if len(cfg.Index.Excludes) == 0 {
		t.Fatal("expected default excludes to be populated")
	}
	if got, want := cfg.Retrieval.TopK, 4; got != want {
		t.Fatalf("retrieval.top_k = %d, want %d", got, want)
	}
	if got, want := cfg.Retrieval.MaxContextChars, 6000; got != want {
		t.Fatalf("retrieval.max_context_chars = %d, want %d", got, want)
	}
}

func TestLoadAppliesRetrievalEnvOverrides(t *testing.T) {
	t.Setenv("RILLAN_OPENAI_API_KEY", "test-key")
	t.Setenv("RILLAN_RETRIEVAL_ENABLED", "true")
	t.Setenv("RILLAN_RETRIEVAL_TOP_K", "7")
	t.Setenv("RILLAN_RETRIEVAL_MAX_CONTEXT_CHARS", "321")

	path := writeTempConfig(t, `runtime:
  vector_store_mode: "embedded"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if !cfg.Retrieval.Enabled {
		t.Fatal("expected retrieval.enabled override to be applied")
	}
	if got, want := cfg.Retrieval.TopK, 7; got != want {
		t.Fatalf("retrieval.top_k = %d, want %d", got, want)
	}
	if got, want := cfg.Retrieval.MaxContextChars, 321; got != want {
		t.Fatalf("retrieval.max_context_chars = %d, want %d", got, want)
	}
}

func TestLoadAppliesLocalModelEnvOverrides(t *testing.T) {
	t.Setenv("RILLAN_OPENAI_API_KEY", "test-key")
	t.Setenv("RILLAN_LOCAL_MODEL_ENABLED", "true")
	t.Setenv("RILLAN_LOCAL_MODEL_BASE_URL", "http://localhost:9999")
	t.Setenv("RILLAN_LOCAL_MODEL_EMBED_MODEL", "custom-embed")
	t.Setenv("RILLAN_LOCAL_MODEL_QUERY_REWRITE_ENABLED", "true")
	t.Setenv("RILLAN_LOCAL_MODEL_QUERY_REWRITE_MODEL", "custom-rewrite")

	path := writeTempConfig(t, `runtime:
  vector_store_mode: "embedded"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if !cfg.LocalModel.Enabled {
		t.Fatal("expected local_model.enabled to be true")
	}
	if got, want := cfg.LocalModel.BaseURL, "http://localhost:9999"; got != want {
		t.Fatalf("local_model.base_url = %q, want %q", got, want)
	}
	if got, want := cfg.LocalModel.EmbedModel, "custom-embed"; got != want {
		t.Fatalf("local_model.embed_model = %q, want %q", got, want)
	}
	if !cfg.LocalModel.QueryRewrite.Enabled {
		t.Fatal("expected query_rewrite.enabled to be true")
	}
	if got, want := cfg.LocalModel.QueryRewrite.Model, "custom-rewrite"; got != want {
		t.Fatalf("query_rewrite.model = %q, want %q", got, want)
	}
}

func TestLoadAppliesLocalModelDefaults(t *testing.T) {
	t.Setenv("RILLAN_OPENAI_API_KEY", "test-key")

	path := writeTempConfig(t, `runtime:
  vector_store_mode: "embedded"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.LocalModel.Enabled {
		t.Fatal("expected local_model.enabled to default to false")
	}
	if got, want := cfg.LocalModel.BaseURL, "http://127.0.0.1:11434"; got != want {
		t.Fatalf("local_model.base_url = %q, want %q", got, want)
	}
	if got, want := cfg.LocalModel.EmbedModel, "nomic-embed-text"; got != want {
		t.Fatalf("local_model.embed_model = %q, want %q", got, want)
	}
	if got, want := cfg.LocalModel.QueryRewrite.Model, "qwen3:0.6b"; got != want {
		t.Fatalf("query_rewrite.model = %q, want %q", got, want)
	}
	if cfg.Agent.Enabled {
		t.Fatal("expected agent.enabled to default to false")
	}
	if cfg.Agent.MCP.Enabled {
		t.Fatal("expected agent.mcp.enabled to default to false")
	}
	if !cfg.Agent.MCP.ReadOnly {
		t.Fatal("expected agent.mcp.read_only to default to true")
	}
}

func TestLoadAppliesAgentEnvOverrides(t *testing.T) {
	t.Setenv("RILLAN_OPENAI_API_KEY", "test-key")
	t.Setenv("RILLAN_AGENT_ENABLED", "true")
	t.Setenv("RILLAN_AGENT_MCP_ENABLED", "true")
	t.Setenv("RILLAN_AGENT_MCP_READ_ONLY", "true")
	t.Setenv("RILLAN_AGENT_MCP_MAX_OPEN_FILES", "3")
	t.Setenv("RILLAN_AGENT_MCP_MAX_DIAGNOSTICS", "4")

	path := writeTempConfig(t, `runtime:
  vector_store_mode: "embedded"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !cfg.Agent.Enabled || !cfg.Agent.MCP.Enabled {
		t.Fatal("expected agent and mcp env overrides to be applied")
	}
	if got, want := cfg.Agent.MCP.MaxOpenFiles, 3; got != want {
		t.Fatalf("agent.mcp.max_open_files = %d, want %d", got, want)
	}
	if got, want := cfg.Agent.MCP.MaxDiagnostics, 4; got != want {
		t.Fatalf("agent.mcp.max_diagnostics = %d, want %d", got, want)
	}
}

func TestLoadResolvesRelativeIndexRootFromConfigDirectory(t *testing.T) {
	t.Setenv("RILLAN_OPENAI_API_KEY", "test-key")

	configDir := t.TempDir()
	path := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(path, []byte(`index:
  root: "../vault"
runtime:
  vector_store_mode: "embedded"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	want := filepath.Clean(filepath.Join(configDir, "..", "vault"))
	if got := cfg.Index.Root; got != want {
		t.Fatalf("index root = %q, want %q", got, want)
	}
}

func TestDefaultPathsAreNonEmpty(t *testing.T) {
	if DefaultConfigPath() == "" {
		t.Fatal("DefaultConfigPath returned empty string")
	}
	if DefaultSystemConfigPath() == "" {
		t.Fatal("DefaultSystemConfigPath returned empty string")
	}
	if DefaultDataDir() == "" {
		t.Fatal("DefaultDataDir returned empty string")
	}
	if DefaultLogDir() == "" {
		t.Fatal("DefaultLogDir returned empty string")
	}
}

func TestLoadInitializesSchemaV2Registries(t *testing.T) {
	t.Setenv("RILLAN_OPENAI_API_KEY", "test-key")

	path := writeTempConfig(t, `runtime:
  vector_store_mode: "embedded"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.LLMs.Providers == nil {
		t.Fatal("expected llm providers to be initialized")
	}
	if got, want := len(cfg.LLMs.Providers), 6; got != want {
		t.Fatalf("len(llms.providers) = %d, want %d", got, want)
	}
	if cfg.MCPs.Servers == nil {
		t.Fatal("expected mcp servers to be initialized")
	}
}

func TestLoadForEditReturnsDefaultsWhenConfigMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "config.yaml")

	cfg, err := LoadForEdit(path)
	if err != nil {
		t.Fatalf("LoadForEdit returned error: %v", err)
	}

	if got, want := cfg.SchemaVersion, SchemaVersionV2; got != want {
		t.Fatalf("schema_version = %d, want %d", got, want)
	}
	if cfg.LLMs.Providers == nil {
		t.Fatal("expected llm providers to be initialized")
	}
	if got, want := len(cfg.LLMs.Providers), 6; got != want {
		t.Fatalf("len(llms.providers) = %d, want %d", got, want)
	}
	if cfg.MCPs.Servers == nil {
		t.Fatal("expected mcp servers to be initialized")
	}
}

func TestWritePersistsSchemaV2Config(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := DefaultConfig()
	cfg.LLMs.Default = "work-gpt"
	cfg.LLMs.Providers = append(cfg.LLMs.Providers, LLMProviderConfig{
		ID:           "work-gpt",
		Backend:      LLMBackendOpenAICompatible,
		Transport:    LLMTransportHTTP,
		Endpoint:     "https://api.openai.com/v1",
		AuthStrategy: AuthStrategyBrowserOIDC,
	})

	if err := Write(path, cfg); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	reloaded, err := LoadForEdit(path)
	if err != nil {
		t.Fatalf("LoadForEdit returned error: %v", err)
	}

	if got, want := reloaded.LLMs.Default, "work-gpt"; got != want {
		t.Fatalf("llms.default = %q, want %q", got, want)
	}
	if got, want := len(reloaded.LLMs.Providers), 7; got != want {
		t.Fatalf("len(llms.providers) = %d, want %d", got, want)
	}
}

func TestLoadAppliesBundledPresetDefaults(t *testing.T) {
	path := writeTempConfig(t, `schema_version: 2
llms:
  default: "deepseek-work"
  providers:
    - id: "deepseek-work"
      preset: "deepseek"
runtime:
  vector_store_mode: "embedded"
`)

	cfg, err := LoadForEdit(path)
	if err != nil {
		t.Fatalf("LoadForEdit returned error: %v", err)
	}

	provider := cfg.LLMs.Providers[0]
	if got, want := provider.Backend, LLMBackendOpenAICompatible; got != want {
		t.Fatalf("backend = %q, want %q", got, want)
	}
	if got, want := provider.Transport, LLMTransportHTTP; got != want {
		t.Fatalf("transport = %q, want %q", got, want)
	}
	if got, want := provider.Endpoint, "https://api.deepseek.com/v1"; got != want {
		t.Fatalf("endpoint = %q, want %q", got, want)
	}
	if got, want := provider.AuthStrategy, AuthStrategyAPIKey; got != want {
		t.Fatalf("auth_strategy = %q, want %q", got, want)
	}
	if got, want := provider.CredentialRef, "keyring://rillan/llm/deepseek-work"; got != want {
		t.Fatalf("credential_ref = %q, want %q", got, want)
	}
}

func TestLoadResolvesSelectedLLMProviderHostFromCredentialStore(t *testing.T) {
	store := map[string]string{}
	secretstore.SetKeyringSetForTest(func(service string, user string, password string) error {
		store[service+"/"+user] = password
		return nil
	})
	secretstore.SetKeyringGetForTest(func(service string, user string) (string, error) {
		value, ok := store[service+"/"+user]
		if !ok {
			return "", keyring.ErrNotFound
		}
		return value, nil
	})
	t.Cleanup(func() {
		secretstore.SetKeyringSetForTest(keyring.Set)
		secretstore.SetKeyringGetForTest(keyring.Get)
	})

	if err := secretstore.Save("keyring://rillan/llm/work-gpt", secretstore.Credential{Kind: "api_key", APIKey: "secret-key", Endpoint: "https://api.openai.com/v1", AuthStrategy: AuthStrategyAPIKey}); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	path := writeTempConfig(t, `schema_version: 2
llms:
  default: "work-gpt"
  providers:
    - id: "work-gpt"
      backend: "openai_compatible"
      transport: "http"
      endpoint: "https://api.openai.com/v1"
      auth_strategy: "api_key"
      credential_ref: "keyring://rillan/llm/work-gpt"
runtime:
  vector_store_mode: "embedded"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	resolved, err := ResolveRuntimeProviderHostConfig(cfg, DefaultProjectConfig())
	if err != nil {
		t.Fatalf("ResolveRuntimeProviderHostConfig returned error: %v", err)
	}
	if got, want := resolved.Default, "work-gpt"; got != want {
		t.Fatalf("default provider = %q, want %q", got, want)
	}
	if got, want := len(resolved.Providers), 1; got != want {
		t.Fatalf("provider count = %d, want %d", got, want)
	}
	if got, want := resolved.Providers[0].Type, ProviderOpenAICompatible; got != want {
		t.Fatalf("provider.type = %q, want %q", got, want)
	}
	if got, want := resolved.Providers[0].OpenAI.APIKey, "secret-key"; got != want {
		t.Fatalf("provider.openai.api_key = %q, want %q", got, want)
	}
}

func TestResolveRuntimeProviderHostConfigUsesPresetFamilyForXAI(t *testing.T) {
	store := map[string]string{}
	secretstore.SetKeyringSetForTest(func(service string, user string, password string) error {
		store[service+"/"+user] = password
		return nil
	})
	secretstore.SetKeyringGetForTest(func(service string, user string) (string, error) {
		value, ok := store[service+"/"+user]
		if !ok {
			return "", keyring.ErrNotFound
		}
		return value, nil
	})
	t.Cleanup(func() {
		secretstore.SetKeyringSetForTest(keyring.Set)
		secretstore.SetKeyringGetForTest(keyring.Get)
	})

	if err := secretstore.Save("keyring://rillan/llm/xai-work", secretstore.Credential{Kind: "api_key", APIKey: "xai-secret", Endpoint: "https://api.x.ai/v1", AuthStrategy: AuthStrategyAPIKey}); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	path := writeTempConfig(t, `schema_version: 2
llms:
  default: "xai-work"
  providers:
    - id: "xai-work"
      preset: "xai"
      credential_ref: "keyring://rillan/llm/xai-work"
runtime:
  vector_store_mode: "embedded"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	resolved, err := ResolveRuntimeProviderHostConfig(cfg, DefaultProjectConfig())
	if err != nil {
		t.Fatalf("ResolveRuntimeProviderHostConfig returned error: %v", err)
	}
	if got, want := resolved.Providers[0].Type, ProviderOpenAICompatible; got != want {
		t.Fatalf("provider.type = %q, want %q", got, want)
	}
	if got, want := resolved.Providers[0].Preset, LLMPresetXAI; got != want {
		t.Fatalf("provider.preset = %q, want %q", got, want)
	}
	if got, want := resolved.Providers[0].OpenAI.BaseURL, "https://api.x.ai/v1"; got != want {
		t.Fatalf("provider.openai.base_url = %q, want %q", got, want)
	}
	if got, want := resolved.Providers[0].OpenAI.APIKey, "xai-secret"; got != want {
		t.Fatalf("provider.openai.api_key = %q, want %q", got, want)
	}
}

func TestResolveRuntimeProviderHostConfigUsesAnthropicPresetFamily(t *testing.T) {
	store := map[string]string{}
	secretstore.SetKeyringSetForTest(func(service string, user string, password string) error {
		store[service+"/"+user] = password
		return nil
	})
	secretstore.SetKeyringGetForTest(func(service string, user string) (string, error) {
		value, ok := store[service+"/"+user]
		if !ok {
			return "", keyring.ErrNotFound
		}
		return value, nil
	})
	t.Cleanup(func() {
		secretstore.SetKeyringSetForTest(keyring.Set)
		secretstore.SetKeyringGetForTest(keyring.Get)
	})

	if err := secretstore.Save("keyring://rillan/llm/anthropic-work", secretstore.Credential{Kind: "api_key", APIKey: "anthropic-secret", Endpoint: "https://api.anthropic.com", AuthStrategy: AuthStrategyAPIKey}); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	path := writeTempConfig(t, `schema_version: 2
llms:
  default: "anthropic-work"
  providers:
    - id: "anthropic-work"
      preset: "anthropic"
      credential_ref: "keyring://rillan/llm/anthropic-work"
runtime:
  vector_store_mode: "embedded"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	resolved, err := ResolveRuntimeProviderHostConfig(cfg, DefaultProjectConfig())
	if err != nil {
		t.Fatalf("ResolveRuntimeProviderHostConfig returned error: %v", err)
	}
	if got, want := resolved.Providers[0].Type, ProviderAnthropic; got != want {
		t.Fatalf("provider.type = %q, want %q", got, want)
	}
	if got, want := resolved.Providers[0].Preset, LLMPresetAnthropic; got != want {
		t.Fatalf("provider.preset = %q, want %q", got, want)
	}
	if got, want := resolved.Providers[0].Anthropic.BaseURL, "https://api.anthropic.com"; got != want {
		t.Fatalf("provider.anthropic.base_url = %q, want %q", got, want)
	}
	if got, want := resolved.Providers[0].Anthropic.APIKey, "anthropic-secret"; got != want {
		t.Fatalf("provider.anthropic.api_key = %q, want %q", got, want)
	}
	if got := resolved.Providers[0].OpenAI.BaseURL; got != "" {
		t.Fatalf("provider.openai.base_url = %q, want empty", got)
	}
}

func TestResolveRuntimeProviderHostConfigUsesInternalOllamaFamily(t *testing.T) {
	path := writeTempConfig(t, `schema_version: 2
llms:
  default: "local-chat"
  providers:
    - id: "local-chat"
      backend: "ollama"
      transport: "http"
      default_model: "qwen3:8b"
local_model:
  enabled: true
  base_url: "http://127.0.0.1:11434"
  embed_model: "nomic-embed-text"
runtime:
  vector_store_mode: "embedded"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	resolved, err := ResolveRuntimeProviderHostConfig(cfg, DefaultProjectConfig())
	if err != nil {
		t.Fatalf("ResolveRuntimeProviderHostConfig returned error: %v", err)
	}
	if got, want := resolved.Default, "local-chat"; got != want {
		t.Fatalf("default provider = %q, want %q", got, want)
	}
	if got, want := resolved.Providers[0].Type, ProviderOllama; got != want {
		t.Fatalf("provider.type = %q, want %q", got, want)
	}
	if got, want := resolved.Providers[0].LocalModel.BaseURL, "http://127.0.0.1:11434"; got != want {
		t.Fatalf("provider.local_model.base_url = %q, want %q", got, want)
	}
	if got := resolved.Providers[0].OpenAI.BaseURL; got != "" {
		t.Fatalf("provider.openai.base_url = %q, want empty", got)
	}
	if got := resolved.Providers[0].Anthropic.BaseURL; got != "" {
		t.Fatalf("provider.anthropic.base_url = %q, want empty", got)
	}
}

func TestResolveRuntimeProviderHostConfigHonorsProjectAllowlist(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LLMs.Default = "openai"
	project := DefaultProjectConfig()
	project.Providers.LLMAllowed = []string{"local-only"}

	if _, err := ResolveRuntimeProviderHostConfig(cfg, project); err == nil {
		t.Fatal("expected project llm allowlist to reject the default provider")
	}
}

func TestResolveRuntimeProviderHostConfigIncludesEligibleAllowlistedProviders(t *testing.T) {
	path := writeTempConfig(t, `schema_version: 2
llms:
  default: "remote-gpt"
  providers:
    - id: "remote-gpt"
      backend: "openai_compatible"
      transport: "http"
      endpoint: "https://api.openai.com/v1"
      auth_strategy: "none"
    - id: "local-qwen"
      backend: "ollama"
      transport: "http"
      endpoint: "http://127.0.0.1:11434"
      default_model: "qwen3:8b"
    - id: "stdio-future"
      backend: "openai_compatible"
      transport: "stdio"
      command: ["future-provider"]
runtime:
  vector_store_mode: "embedded"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	project := DefaultProjectConfig()
	project.Providers.LLMAllowed = []string{"remote-gpt", "local-qwen", "stdio-future"}

	resolved, err := ResolveRuntimeProviderHostConfig(cfg, project)
	if err != nil {
		t.Fatalf("ResolveRuntimeProviderHostConfig returned error: %v", err)
	}
	if got, want := resolved.Default, "remote-gpt"; got != want {
		t.Fatalf("default provider = %q, want %q", got, want)
	}
	if got, want := len(resolved.Providers), 2; got != want {
		t.Fatalf("provider count = %d, want %d", got, want)
	}
	if got, want := resolved.Providers[0].ID, "remote-gpt"; got != want {
		t.Fatalf("providers[0].id = %q, want %q", got, want)
	}
	if got, want := resolved.Providers[1].ID, "local-qwen"; got != want {
		t.Fatalf("providers[1].id = %q, want %q", got, want)
	}
}

func TestLoadProjectAppliesDefaultsAndResolvesRelativeSourcePaths(t *testing.T) {
	projectDir := t.TempDir()
	projectPath := filepath.Join(projectDir, ".rillan", "project.yaml")
	if err := os.MkdirAll(filepath.Dir(projectPath), 0o755); err != nil {
		t.Fatalf("mkdir project config dir: %v", err)
	}
	if err := os.WriteFile(projectPath, []byte("name: \"demo\"\nsources:\n  - path: \"src\"\n    type: \"go\"\n"), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	cfg, err := LoadProject(projectPath)
	if err != nil {
		t.Fatalf("LoadProject returned error: %v", err)
	}

	if got, want := cfg.Classification, ProjectClassificationOpenSource; got != want {
		t.Fatalf("classification = %q, want %q", got, want)
	}
	if got, want := cfg.Routing.Default, RoutePreferenceAuto; got != want {
		t.Fatalf("routing.default = %q, want %q", got, want)
	}
	if got, want := cfg.Sources[0].Path, filepath.Join(projectDir, ".rillan", "src"); got != want {
		t.Fatalf("sources[0].path = %q, want %q", got, want)
	}
}

func TestLoadProjectInitializesProviderAndSkillSelections(t *testing.T) {
	projectDir := t.TempDir()
	projectPath := filepath.Join(projectDir, ".rillan", "project.yaml")
	if err := os.MkdirAll(filepath.Dir(projectPath), 0o755); err != nil {
		t.Fatalf("mkdir project config dir: %v", err)
	}
	if err := os.WriteFile(projectPath, []byte("name: \"demo\"\n"), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	cfg, err := LoadProject(projectPath)
	if err != nil {
		t.Fatalf("LoadProject returned error: %v", err)
	}

	if cfg.Providers.LLMAllowed == nil {
		t.Fatal("expected providers.llm_allowed to be initialized")
	}
	if cfg.Providers.MCPEnabled == nil {
		t.Fatal("expected providers.mcp_enabled to be initialized")
	}
	if cfg.Agent.Skills.Enabled == nil {
		t.Fatal("expected agent.skills.enabled to be initialized")
	}
}

func TestLoadProjectDoesNotUseEnvironmentOverrides(t *testing.T) {
	t.Setenv("RILLAN_PROJECT_NAME", "env-project")

	projectPath := writeTempProjectConfig(t, "name: \"file-project\"\nclassification: \"internal\"\n")

	cfg, err := LoadProject(projectPath)
	if err != nil {
		t.Fatalf("LoadProject returned error: %v", err)
	}

	if got, want := cfg.Name, "file-project"; got != want {
		t.Fatalf("name = %q, want %q", got, want)
	}
}

func TestDefaultProjectConfigPathUsesRootWhenProvided(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	if got, want := DefaultProjectConfigPath(root), filepath.Join(root, ".rillan", "project.yaml"); got != want {
		t.Fatalf("DefaultProjectConfigPath() = %q, want %q", got, want)
	}
}

func TestResolveProjectConfigPathFallsBackToLegacySidekickLocation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	legacy := filepath.Join(root, ".sidekick", "project.yaml")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatalf("mkdir legacy config dir: %v", err)
	}
	if err := os.WriteFile(legacy, []byte("name: \"demo\"\n"), 0o644); err != nil {
		t.Fatalf("write legacy project config: %v", err)
	}
	if got, want := ResolveProjectConfigPath(root), legacy; got != want {
		t.Fatalf("ResolveProjectConfigPath() = %q, want %q", got, want)
	}
}

func TestLoadProjectRejectsMalformedYAML(t *testing.T) {
	projectPath := writeTempProjectConfig(t, "name: [oops\n")

	if _, err := LoadProject(projectPath); err == nil {
		t.Fatal("expected malformed project YAML to fail")
	}
}

func TestLoadProjectRejectsInvalidProjectConfig(t *testing.T) {
	projectPath := writeTempProjectConfig(t, "name: \"demo\"\nclassification: \"unknown\"\n")

	if _, err := LoadProject(projectPath); err == nil {
		t.Fatal("expected invalid project config to fail")
	}
}

func TestLoadSystemAppliesDefaults(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	payload, err := encryptSystemPolicyPayloadForTest(SystemPolicy{
		Identity: SystemIdentityRules{People: []string{"alice@example.com"}},
		Rules:    SystemPolicyRules{MaskPIIForRemote: true},
	}, key, strings.NewReader("123456789012"))
	if err != nil {
		t.Fatalf("encryptSystemPolicyPayload returned error: %v", err)
	}
	withSystemKeyringGet(t, func(service, account string) (string, error) {
		return hex.EncodeToString(key), nil
	})

	systemPath := writeTempSystemConfig(t, "encrypted_payload: \""+payload+"\"\n")

	cfg, err := LoadSystem(systemPath)
	if err != nil {
		t.Fatalf("LoadSystem returned error: %v", err)
	}

	if got, want := cfg.Version, SystemConfigVersion; got != want {
		t.Fatalf("version = %q, want %q", got, want)
	}
	if got, want := cfg.Encryption.Method, SystemEncryptionKeyringAESGCM; got != want {
		t.Fatalf("encryption.method = %q, want %q", got, want)
	}
	if got, want := cfg.Encryption.KeyringService, DefaultSystemKeyringService; got != want {
		t.Fatalf("keyring_service = %q, want %q", got, want)
	}
	if got, want := cfg.Encryption.KeyringAccount, DefaultSystemKeyringAccount; got != want {
		t.Fatalf("keyring_account = %q, want %q", got, want)
	}
	if got, want := cfg.Policy.Identity.People[0], "alice@example.com"; got != want {
		t.Fatalf("policy identity = %q, want %q", got, want)
	}
}

func TestLoadSystemRejectsPlaintextPolicyData(t *testing.T) {
	systemPath := writeTempSystemConfig(t, "identity:\n  people:\n    - \"name@example.com\"\nencrypted_payload: \"ciphertext\"\n")

	if _, err := LoadSystem(systemPath); err == nil {
		t.Fatal("expected plaintext system data to fail")
	}
}

func TestLoadSystemRejectsMalformedYAML(t *testing.T) {
	systemPath := writeTempSystemConfig(t, "version: [oops\n")

	if _, err := LoadSystem(systemPath); err == nil {
		t.Fatal("expected malformed system YAML to fail")
	}
}

func TestLoadSystemRejectsWrongKey(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	payload, err := encryptSystemPolicyPayloadForTest(SystemPolicy{Rules: SystemPolicyRules{MaskPIIForRemote: true}}, key, strings.NewReader("123456789012"))
	if err != nil {
		t.Fatalf("encryptSystemPolicyPayload returned error: %v", err)
	}
	withSystemKeyringGet(t, func(service, account string) (string, error) {
		return hex.EncodeToString([]byte("abcdef0123456789abcdef0123456789")), nil
	})

	systemPath := writeTempSystemConfig(t, "encrypted_payload: \""+payload+"\"\n")
	if _, err := LoadSystem(systemPath); err == nil {
		t.Fatal("expected wrong key to fail decryption")
	}
}

func TestLoadSystemRejectsMalformedCiphertext(t *testing.T) {
	withSystemKeyringGet(t, func(service, account string) (string, error) {
		return hex.EncodeToString([]byte("0123456789abcdef0123456789abcdef")), nil
	})

	systemPath := writeTempSystemConfig(t, "encrypted_payload: \"not-base64\"\n")
	if _, err := LoadSystem(systemPath); err == nil {
		t.Fatal("expected malformed ciphertext to fail")
	}
}

func encryptSystemPolicyPayloadForTest(policy SystemPolicy, key []byte, random io.Reader) (string, error) {
	plaintext, err := json.Marshal(policy)
	if err != nil {
		return "", fmt.Errorf("marshal system policy payload: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create aes-gcm cipher: %w", err)
	}
	if random == nil {
		random = rand.Reader
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(random, nonce); err != nil {
		return "", fmt.Errorf("read nonce: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	combined := append(nonce, ciphertext...)
	return base64.StdEncoding.EncodeToString(combined), nil
}

func TestLoadSystemMissingReturnsNotExist(t *testing.T) {
	missing := filepath.Join(t.TempDir(), ".sidekick", "system.yaml")

	if _, err := LoadSystem(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("LoadSystem error = %v, want os.ErrNotExist", err)
	}
}

func TestLoadSystemRejectsMissingKeyringMaterial(t *testing.T) {
	withSystemKeyringGet(t, func(service, account string) (string, error) {
		return "", errors.New("missing")
	})

	systemPath := writeTempSystemConfig(t, "encrypted_payload: \"Y2lwaGVydGV4dA==\"\n")
	if _, err := LoadSystem(systemPath); err == nil {
		t.Fatal("expected missing keyring material to fail")
	}
}

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	return path
}

func writeTempProjectConfig(t *testing.T, content string) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), ".sidekick")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir temp project dir: %v", err)
	}

	path := filepath.Join(dir, "project.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp project config: %v", err)
	}

	return path
}

func writeTempSystemConfig(t *testing.T, content string) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), ".sidekick")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir temp system dir: %v", err)
	}

	path := filepath.Join(dir, "system.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp system config: %v", err)
	}

	return path
}

func withSystemKeyringGet(t *testing.T, fn func(service, account string) (string, error)) {
	t.Helper()
	original := systemKeyringGet
	systemKeyringGet = fn
	t.Cleanup(func() {
		systemKeyringGet = original
	})
}
