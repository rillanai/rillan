package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/rillanai/rillan/internal/chat"
	"github.com/rillanai/rillan/internal/config"
	"github.com/rillanai/rillan/internal/modules"
	internalopenai "github.com/rillanai/rillan/internal/openai"
	"github.com/rillanai/rillan/internal/secretstore"
	keyring "github.com/zalando/go-keyring"
)

func stubSnapshotKeyring(t *testing.T) {
	t.Helper()
	secretstore.SetKeyringGetForTest(func(service string, user string) (string, error) {
		return `{"api_key":"test-key","auth_strategy":"api_key"}`, nil
	})
	t.Cleanup(func() { secretstore.SetKeyringGetForTest(keyring.Get) })
}

func defaultV1SnapshotTestConfig(serverURL string) config.Config {
	cfg := config.DefaultConfig()
	cfg.SchemaVersion = config.SchemaVersionV1
	cfg.LLMs = config.LLMRegistryConfig{}
	cfg.Provider.Type = config.ProviderOpenAI
	cfg.Provider.OpenAI.APIKey = "test-key"
	cfg.Provider.OpenAI.BaseURL = serverURL
	return cfg
}

func trustedSystemForModule(t *testing.T, projectPath string, moduleID string, allowStdio bool) *config.SystemConfig {
	t.Helper()
	catalog, err := modules.LoadProjectCatalog(projectPath)
	if err != nil {
		t.Fatalf("LoadProjectCatalog returned error: %v", err)
	}
	if len(catalog.Modules) != 1 {
		t.Fatalf("expected exactly one module, got %d", len(catalog.Modules))
	}
	return &config.SystemConfig{Policy: config.SystemPolicy{TrustedModules: []config.TrustedModulePolicy{{
		RepoRoot:       modules.ProjectRootFromConfigPath(projectPath),
		ModuleID:       moduleID,
		ManifestSHA256: catalog.Modules[0].ManifestSHA256,
		AllowStdio:     allowStdio,
	}}}}
}

func TestBuildRuntimeSnapshotLoadsProjectModules(t *testing.T) {
	stubSnapshotKeyring(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, ".rillan", "project.yaml")
	manifestPath := filepath.Join(projectRoot, ".rillan", "modules", "demo", "module.yaml")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte(`id: "demo"
version: "0.1.0"
entrypoint: ["./bin/module"]
llm_adapters:
  - id: "demo-http"
    backend: "openai_compatible"
    transport: "http"
    endpoint: "https://example.com/v1"
`), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	cfg := config.DefaultConfig()
	project := config.DefaultProjectConfig()
	project.Name = "demo"
	project.Modules.Enabled = []string{"demo"}

	snapshot, err := buildRuntimeSnapshot(context.Background(), cfg, project, trustedSystemForModule(t, projectPath, "demo", false), filepath.Join(t.TempDir(), "audit.jsonl"), projectPath)
	if err != nil {
		t.Fatalf("buildRuntimeSnapshot returned error: %v", err)
	}
	if got, want := len(snapshot.Modules.Modules), 1; got != want {
		t.Fatalf("len(snapshot.Modules.Modules) = %d, want %d", got, want)
	}
	if got, want := snapshot.Modules.Modules[0].ID, "demo"; got != want {
		t.Fatalf("snapshot.Modules.Modules[0].ID = %q, want %q", got, want)
	}
	if got, want := snapshot.Modules.Modules[0].LLMAdapters[0].ID, "demo-http"; got != want {
		t.Fatalf("snapshot.Modules.Modules[0].LLMAdapters[0].ID = %q, want %q", got, want)
	}
	if got, want := snapshot.ReadinessInfo.ModulesDiscovered, 1; got != want {
		t.Fatalf("snapshot.ReadinessInfo.ModulesDiscovered = %d, want %d", got, want)
	}
	if got, want := snapshot.ReadinessInfo.ModulesEnabled, 1; got != want {
		t.Fatalf("snapshot.ReadinessInfo.ModulesEnabled = %d, want %d", got, want)
	}
}

func TestBuildRuntimeSnapshotLeavesModulesInactiveWhenProjectHasNoEnabledModules(t *testing.T) {
	stubSnapshotKeyring(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, ".rillan", "project.yaml")
	manifestPath := filepath.Join(projectRoot, ".rillan", "modules", "demo", "module.yaml")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte("id: \"demo\"\nversion: \"0.1.0\"\nentrypoint: [\"./bin/module\"]\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	cfg := config.DefaultConfig()
	project := config.DefaultProjectConfig()
	project.Name = "demo"

	snapshot, err := buildRuntimeSnapshot(context.Background(), cfg, project, nil, filepath.Join(t.TempDir(), "audit.jsonl"), projectPath)
	if err != nil {
		t.Fatalf("buildRuntimeSnapshot returned error: %v", err)
	}
	if got := len(snapshot.Modules.Modules); got != 0 {
		t.Fatalf("len(snapshot.Modules.Modules) = %d, want 0", got)
	}
	if got, want := snapshot.ReadinessInfo.ModulesDiscovered, 1; got != want {
		t.Fatalf("snapshot.ReadinessInfo.ModulesDiscovered = %d, want %d", got, want)
	}
	if got, want := snapshot.ReadinessInfo.ModulesEnabled, 0; got != want {
		t.Fatalf("snapshot.ReadinessInfo.ModulesEnabled = %d, want %d", got, want)
	}
}

func TestBuildRuntimeSnapshotRejectsUnknownEnabledModule(t *testing.T) {
	stubSnapshotKeyring(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := config.DefaultConfig()
	project := config.DefaultProjectConfig()
	project.Name = "demo"
	project.Modules.Enabled = []string{"missing"}

	if _, err := buildRuntimeSnapshot(context.Background(), cfg, project, nil, filepath.Join(t.TempDir(), "audit.jsonl"), filepath.Join(t.TempDir(), ".rillan", "project.yaml")); err == nil {
		t.Fatal("expected unknown enabled module to fail")
	}
}

func TestBuildRuntimeSnapshotRejectsEnabledButUntrustedModule(t *testing.T) {
	stubSnapshotKeyring(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, ".rillan", "project.yaml")
	manifestPath := filepath.Join(projectRoot, ".rillan", "modules", "demo", "module.yaml")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte("id: \"demo\"\nversion: \"0.1.0\"\nentrypoint: [\"./bin/module\"]\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	cfg := config.DefaultConfig()
	project := config.DefaultProjectConfig()
	project.Name = "demo"
	project.Modules.Enabled = []string{"demo"}

	if _, err := buildRuntimeSnapshot(context.Background(), cfg, project, &config.SystemConfig{}, filepath.Join(t.TempDir(), "audit.jsonl"), projectPath); err == nil {
		t.Fatal("expected untrusted enabled module to fail")
	}
}

func TestBuildRuntimeSnapshotUsesEnabledModuleHTTPAdapterAsSelectedProvider(t *testing.T) {
	requests := make(chan struct {
		path string
		body string
	}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/chat/completions" {
			requests <- struct {
				path string
				body string
			}{path: r.URL.Path, body: string(body)}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"ok"}`))
	}))
	defer server.Close()

	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, ".rillan", "project.yaml")
	manifestPath := filepath.Join(projectRoot, ".rillan", "modules", "demo", "module.yaml")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte(`id: "demo"
version: "0.1.0"
entrypoint: ["./bin/module"]
llm_adapters:
  - id: "demo-http"
    backend: "openai_compatible"
    transport: "http"
    endpoint: "`+server.URL+`"
    auth_strategy: "none"
    default_model: "demo-model"
`), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	cfg := config.DefaultConfig()
	project := config.DefaultProjectConfig()
	project.Name = "demo"
	project.Modules.Enabled = []string{"demo"}
	project.Providers.LLMDefault = "demo-http"
	project.Providers.LLMAllowed = []string{"demo-http"}

	snapshot, err := buildRuntimeSnapshot(context.Background(), cfg, project, trustedSystemForModule(t, projectPath, "demo", false), filepath.Join(t.TempDir(), "audit.jsonl"), projectPath)
	if err != nil {
		t.Fatalf("buildRuntimeSnapshot returned error: %v", err)
	}
	if _, ok := snapshot.RouteCatalog.ByID["demo-http"]; !ok {
		t.Fatal("expected route catalog to include module adapter")
	}
	status, ok := snapshot.RouteStatus.ByID["demo-http"]
	if !ok {
		t.Fatal("expected route status to include module adapter")
	}
	if !status.Available {
		t.Fatalf("expected module adapter route status to be available, got %#v", status)
	}

	response, err := snapshot.Provider.ChatCompletions(context.Background(), chat.ProviderRequest{Request: internalopenai.ChatCompletionRequest{Model: "demo-model"}, RawBody: []byte(`{"model":"demo-model","messages":[{"role":"user","content":"ping"}]}`)})
	if err != nil {
		t.Fatalf("ChatCompletions returned error: %v", err)
	}
	defer response.Body.Close()

	req := <-requests
	if got, want := req.path, "/chat/completions"; got != want {
		t.Fatalf("request path = %q, want %q", got, want)
	}
	if got, want := req.body, `{"model":"demo-model","messages":[{"role":"user","content":"ping"}]}`; got != want {
		t.Fatalf("request body = %s, want %s", got, want)
	}
}

func TestBuildRuntimeSnapshotRejectsModuleAdapterIDCollision(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, ".rillan", "project.yaml")
	manifestPath := filepath.Join(projectRoot, ".rillan", "modules", "demo", "module.yaml")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte(`id: "demo"
version: "0.1.0"
entrypoint: ["./bin/module"]
llm_adapters:
  - id: "openai"
    backend: "openai_compatible"
    transport: "http"
    endpoint: "https://example.com/v1"
    auth_strategy: "none"
`), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	cfg := config.DefaultConfig()
	project := config.DefaultProjectConfig()
	project.Name = "demo"
	project.Modules.Enabled = []string{"demo"}
	project.Providers.LLMAllowed = []string{"openai"}

	if _, err := buildRuntimeSnapshot(context.Background(), cfg, project, trustedSystemForModule(t, projectPath, "demo", false), filepath.Join(t.TempDir(), "audit.jsonl"), projectPath); err == nil {
		t.Fatal("expected module adapter id collision to fail")
	}
}

func TestBuildRuntimeSnapshotUsesEnabledModuleStdioAdapterAsSelectedProvider(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is unix-specific")
	}
	stubSnapshotKeyring(t)

	requestPath := filepath.Join(t.TempDir(), "request.json")
	scriptPath := filepath.Join(t.TempDir(), "provider.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nset -eu\ncat > \"$REQUEST_PATH\"\nprintf '%s' '{\"status_code\":200,\"headers\":{\"Content-Type\":[\"application/json\"]},\"body\":{\"id\":\"stdio-resp\"}}'\n"), 0o755); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	t.Setenv("REQUEST_PATH", requestPath)

	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, ".rillan", "project.yaml")
	manifestPath := filepath.Join(projectRoot, ".rillan", "modules", "demo", "module.yaml")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte(`id: "demo"
version: "0.1.0"
entrypoint: ["./bin/module"]
llm_adapters:
  - id: "demo-stdio"
    backend: "openai_compatible"
    transport: "stdio"
    command: ["`+scriptPath+`"]
    auth_strategy: "none"
    default_model: "demo-model"
`), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	cfg := config.DefaultConfig()
	project := config.DefaultProjectConfig()
	project.Name = "demo"
	project.Modules.Enabled = []string{"demo"}
	project.Providers.LLMDefault = "demo-stdio"
	project.Providers.LLMAllowed = []string{"demo-stdio"}

	snapshot, err := buildRuntimeSnapshot(context.Background(), cfg, project, trustedSystemForModule(t, projectPath, "demo", true), filepath.Join(t.TempDir(), "audit.jsonl"), projectPath)
	if err != nil {
		t.Fatalf("buildRuntimeSnapshot returned error: %v", err)
	}
	status, ok := snapshot.RouteStatus.ByID["demo-stdio"]
	if !ok || !status.Available {
		t.Fatalf("expected stdio adapter route status to be available, got %#v", status)
	}

	response, err := snapshot.Provider.ChatCompletions(context.Background(), chat.ProviderRequest{Request: internalopenai.ChatCompletionRequest{Model: "demo-model"}, RawBody: []byte(`{"model":"demo-model"}`)})
	if err != nil {
		t.Fatalf("ChatCompletions returned error: %v", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if got, want := string(responseBody), `{"id":"stdio-resp"}`; got != want {
		t.Fatalf("response body = %s, want %s", got, want)
	}
	requestData, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if got, want := string(requestData), `{"request":{"model":"demo-model","messages":null},"raw_body":{"model":"demo-model"}}`; got != want {
		t.Fatalf("request payload = %s, want %s", got, want)
	}
}

func TestBuildRuntimeSnapshotRejectsTrustedStdioModuleWithoutStdioOptIn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is unix-specific")
	}
	stubSnapshotKeyring(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	projectRoot := t.TempDir()
	projectPath := filepath.Join(projectRoot, ".rillan", "project.yaml")
	manifestPath := filepath.Join(projectRoot, ".rillan", "modules", "demo", "module.yaml")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte(`id: "demo"
version: "0.1.0"
entrypoint: ["./bin/module"]
llm_adapters:
  - id: "demo-stdio"
    backend: "openai_compatible"
    transport: "stdio"
    command: ["/bin/true"]
    auth_strategy: "none"
    default_model: "demo-model"
`), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	cfg := defaultV1SnapshotTestConfig(server.URL)
	project := config.DefaultProjectConfig()
	project.Name = "demo"
	project.Modules.Enabled = []string{"demo"}
	project.Providers.LLMDefault = "demo-stdio"
	project.Providers.LLMAllowed = []string{"demo-stdio"}

	if _, err := buildRuntimeSnapshot(context.Background(), cfg, project, trustedSystemForModule(t, projectPath, "demo", false), filepath.Join(t.TempDir(), "audit.jsonl"), projectPath); err == nil {
		t.Fatal("expected trusted stdio module without stdio opt-in to fail")
	}
}
