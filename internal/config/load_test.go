package config

import (
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestLoadProjectAppliesDefaultsAndResolvesRelativeSourcePaths(t *testing.T) {
	projectDir := t.TempDir()
	projectPath := filepath.Join(projectDir, ".sidekick", "project.yaml")
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
	if got, want := cfg.Sources[0].Path, filepath.Join(projectDir, ".sidekick", "src"); got != want {
		t.Fatalf("sources[0].path = %q, want %q", got, want)
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
	if got, want := DefaultProjectConfigPath(root), filepath.Join(root, ".sidekick", "project.yaml"); got != want {
		t.Fatalf("DefaultProjectConfigPath() = %q, want %q", got, want)
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
	payload, err := encryptSystemPolicyPayload(SystemPolicy{
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
	payload, err := encryptSystemPolicyPayload(SystemPolicy{Rules: SystemPolicyRules{MaskPIIForRemote: true}}, key, strings.NewReader("123456789012"))
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
