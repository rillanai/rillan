package config

import (
	"os"
	"path/filepath"
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
