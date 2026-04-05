package modules

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rillanai/rillan/internal/config"
)

func TestLoadProjectCatalogReturnsDeterministicModules(t *testing.T) {
	projectPath := filepath.Join(t.TempDir(), ".rillan", "project.yaml")
	mustWriteModuleManifest(t, projectPath, "z-last", `id: "z-last"
version: "0.1.0"
entrypoint: ["./bin/module"]
llm_adapters:
  - id: "z-llm"
    backend: "openai_compatible"
    transport: "http"
    endpoint: "https://example.com/v1"
`)
	mustWriteModuleManifest(t, projectPath, "a-first", `id: "a-first"
version: "0.1.0"
entrypoint: ["./bin/module"]
mcp_servers:
  - id: "repo"
    transport: "stdio"
    command: ["./bin/mcp"]
lsp_servers:
  - id: "gopls"
    command: ["./bin/gopls"]
    languages: ["go"]
`)

	catalog, err := LoadProjectCatalog(projectPath)
	if err != nil {
		t.Fatalf("LoadProjectCatalog returned error: %v", err)
	}
	if got, want := len(catalog.Modules), 2; got != want {
		t.Fatalf("len(catalog.Modules) = %d, want %d", got, want)
	}
	if got, want := catalog.Modules[0].ID, "a-first"; got != want {
		t.Fatalf("catalog.Modules[0].ID = %q, want %q", got, want)
	}
	if got, want := catalog.Modules[1].ID, "z-last"; got != want {
		t.Fatalf("catalog.Modules[1].ID = %q, want %q", got, want)
	}
	if got, want := filepath.Base(catalog.Modules[0].Entrypoint[0]), "module"; got != want {
		t.Fatalf("entrypoint basename = %q, want %q", got, want)
	}
	if !filepath.IsAbs(catalog.Modules[0].Entrypoint[0]) {
		t.Fatalf("entrypoint path = %q, want absolute path", catalog.Modules[0].Entrypoint[0])
	}
	if got, want := filepath.Base(catalog.Modules[0].MCPServers[0].Command[0]), "mcp"; got != want {
		t.Fatalf("mcp command basename = %q, want %q", got, want)
	}
	if got, want := filepath.Base(catalog.Modules[0].LSPServers[0].Command[0]), "gopls"; got != want {
		t.Fatalf("lsp command basename = %q, want %q", got, want)
	}
}

func TestLoadProjectCatalogReturnsEmptyWhenModulesDirMissing(t *testing.T) {
	projectPath := filepath.Join(t.TempDir(), ".rillan", "project.yaml")

	catalog, err := LoadProjectCatalog(projectPath)
	if err != nil {
		t.Fatalf("LoadProjectCatalog returned error: %v", err)
	}
	if got := len(catalog.Modules); got != 0 {
		t.Fatalf("len(catalog.Modules) = %d, want 0", got)
	}
}

func TestLoadProjectCatalogRejectsInvalidManifest(t *testing.T) {
	projectPath := filepath.Join(t.TempDir(), ".rillan", "project.yaml")
	mustWriteModuleManifest(t, projectPath, "broken", `id: "broken"
version: ""
entrypoint: ["./bin/module"]
`)

	if _, err := LoadProjectCatalog(projectPath); err == nil {
		t.Fatal("expected invalid manifest to fail")
	}
}

func TestLoadProjectCatalogRejectsDuplicateModuleIDs(t *testing.T) {
	projectPath := filepath.Join(t.TempDir(), ".rillan", "project.yaml")
	manifest := `id: "shared"
version: "0.1.0"
entrypoint: ["./bin/module"]
`
	mustWriteModuleManifest(t, projectPath, "one", manifest)
	mustWriteModuleManifest(t, projectPath, "two", manifest)

	if _, err := LoadProjectCatalog(projectPath); err == nil {
		t.Fatal("expected duplicate module ids to fail")
	}
}

func TestFilterEnabledReturnsOnlyRequestedModules(t *testing.T) {
	catalog := Catalog{
		ModulesDir: "/repo/.rillan/modules",
		Modules: []LoadedModule{
			{ID: "alpha"},
			{ID: "beta"},
		},
	}

	filtered, err := FilterEnabled(catalog, []string{"beta"})
	if err != nil {
		t.Fatalf("FilterEnabled returned error: %v", err)
	}
	if got, want := len(filtered.Modules), 1; got != want {
		t.Fatalf("len(filtered.Modules) = %d, want %d", got, want)
	}
	if got, want := filtered.Modules[0].ID, "beta"; got != want {
		t.Fatalf("filtered.Modules[0].ID = %q, want %q", got, want)
	}
}

func TestFilterEnabledRejectsUnknownModule(t *testing.T) {
	catalog := Catalog{ModulesDir: "/repo/.rillan/modules", Modules: []LoadedModule{{ID: "alpha"}}}

	if _, err := FilterEnabled(catalog, []string{"missing"}); err == nil {
		t.Fatal("expected unknown enabled module to fail")
	}
}

func TestFilterTrustedRejectsUntrustedModule(t *testing.T) {
	projectConfigPath := filepath.Join(t.TempDir(), ".rillan", "project.yaml")
	if err := os.MkdirAll(filepath.Dir(projectConfigPath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(projectConfigPath, []byte("name: demo\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	catalog := Catalog{ModulesDir: "/repo/.rillan/modules", Modules: []LoadedModule{{ID: "alpha", ManifestSHA256: "abc123"}}}

	if _, err := FilterTrusted(catalog, projectConfigPath, &config.SystemConfig{}); err == nil {
		t.Fatal("expected untrusted module to fail")
	}
}

func TestFilterTrustedRejectsStdioModuleWithoutStdioTrust(t *testing.T) {
	projectRoot := t.TempDir()
	projectConfigPath := filepath.Join(projectRoot, ".rillan", "project.yaml")
	if err := os.MkdirAll(filepath.Dir(projectConfigPath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(projectConfigPath, []byte("name: demo\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	catalog := Catalog{ModulesDir: "/repo/.rillan/modules", Modules: []LoadedModule{{ID: "alpha", ManifestSHA256: "abc123", LLMAdapters: []config.LLMProviderConfig{{ID: "alpha-stdio", Transport: config.LLMTransportSTDIO}}}}}
	systemCfg := &config.SystemConfig{Policy: config.SystemPolicy{TrustedModules: []config.TrustedModulePolicy{{RepoRoot: projectRoot, ModuleID: "alpha", ManifestSHA256: "abc123"}}}}

	if _, err := FilterTrusted(catalog, projectConfigPath, systemCfg); err == nil {
		t.Fatal("expected stdio module without stdio trust to fail")
	}
}

func TestFilterTrustedAcceptsTrustedHTTPModule(t *testing.T) {
	projectRoot := t.TempDir()
	projectConfigPath := filepath.Join(projectRoot, ".rillan", "project.yaml")
	if err := os.MkdirAll(filepath.Dir(projectConfigPath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(projectConfigPath, []byte("name: demo\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	catalog := Catalog{ModulesDir: "/repo/.rillan/modules", Modules: []LoadedModule{{ID: "alpha", ManifestSHA256: "abc123", LLMAdapters: []config.LLMProviderConfig{{ID: "alpha-http", Transport: config.LLMTransportHTTP}}}}}
	systemCfg := &config.SystemConfig{Policy: config.SystemPolicy{TrustedModules: []config.TrustedModulePolicy{{RepoRoot: projectRoot, ModuleID: "alpha", ManifestSHA256: "abc123"}}}}

	filtered, err := FilterTrusted(catalog, projectConfigPath, systemCfg)
	if err != nil {
		t.Fatalf("FilterTrusted returned error: %v", err)
	}
	if got, want := len(filtered.Modules), 1; got != want {
		t.Fatalf("len(filtered.Modules) = %d, want %d", got, want)
	}
}

func mustWriteModuleManifest(t *testing.T, projectPath string, moduleDir string, content string) {
	t.Helper()
	manifestPath := filepath.Join(DefaultProjectModulesDir(projectPath), moduleDir, manifestFileName)
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}
