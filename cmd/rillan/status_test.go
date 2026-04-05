package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rillanai/rillan/internal/config"
	"github.com/rillanai/rillan/internal/index"
	keyring "github.com/zalando/go-keyring"
)

func TestStatusCommandShowsCommittedAndFailedAttemptSeparately(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))

	configPath := writeStatusTestConfig(t)
	store, err := index.OpenStore(index.DefaultDBPath())
	if err != nil {
		t.Fatalf("OpenStore returned error: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	runID, err := store.RecordRunStart(ctx, "/committed-root")
	if err != nil {
		t.Fatalf("RecordRunStart returned error: %v", err)
	}
	if err := store.ReplaceAll(ctx,
		[]index.DocumentRecord{{Path: "a.go", ContentHash: "hash", SizeBytes: 10}},
		[]index.ChunkRecord{{ID: "chunk", DocumentPath: "a.go", Ordinal: 0, StartLine: 1, EndLine: 1, Content: "package main", ContentHash: "hash2"}},
		[]index.VectorRecord{{ChunkID: "chunk", Dimensions: 8, Embedding: []byte{1, 2, 3, 4}}},
	); err != nil {
		t.Fatalf("ReplaceAll returned error: %v", err)
	}
	if err := store.RecordRunCompletion(ctx, runID, index.RunStatusSucceeded, 1, 1, 1, ""); err != nil {
		t.Fatalf("RecordRunCompletion returned error: %v", err)
	}

	failedRunID, err := store.RecordRunStart(ctx, "/failed-root")
	if err != nil {
		t.Fatalf("RecordRunStart returned error: %v", err)
	}
	if err := store.RecordRunCompletion(ctx, failedRunID, index.RunStatusFailed, 0, 0, 0, "boom"); err != nil {
		t.Fatalf("RecordRunCompletion returned error: %v", err)
	}

	cmd := newStatusCommand()
	cmd.SetArgs([]string{"--config", configPath})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext returned error: %v", err)
	}

	output := stdout.String()
	for _, want := range []string{
		"configured_root: /configured-root",
		"last_attempt_state: failed",
		"last_attempt_root: /failed-root",
		"last_attempt_error: boom",
		"committed_root: /committed-root",
		"documents: 1",
		"chunks: 1",
		"vectors: 1",
		"system_config_state: missing",
		"retrieval_mode: disabled",
		"audit_ledger_path:",
		"runtime_state: ready",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestStatusCommandReportsReachableLocalModel(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/" {
			_, _ = w.Write([]byte("ok"))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float64{{0.1}}})
	}))
	defer server.Close()

	configPath := writeLocalModelStatusConfig(t, server.URL)
	cmd := newStatusCommand()
	cmd.SetArgs([]string{"--config", configPath})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext returned error: %v", err)
	}

	output := stdout.String()
	for _, want := range []string{"local_model_enabled: true", "local_model_required: true", "local_model_reachable: true", "local_model_url: " + server.URL, "runtime_state: ready"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestStatusCommandReportsUnreachableLocalModel(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))

	configPath := writeLocalModelStatusConfig(t, "http://127.0.0.1:0")
	cmd := newStatusCommand()
	cmd.SetArgs([]string{"--config", configPath})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext returned error: %v", err)
	}

	output := stdout.String()
	for _, want := range []string{"local_model_enabled: true", "local_model_required: true", "local_model_reachable: false", "runtime_state: degraded"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestStatusCommandReportsInvalidSystemConfigWithoutKeyringMaterial(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	home := t.TempDir()
	t.Setenv("HOME", home)

	systemPath := filepath.Join(home, ".rillan", "system.yaml")
	if err := os.MkdirAll(filepath.Dir(systemPath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(systemPath, []byte("encrypted_payload: \"ciphertext\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	configPath := writeStatusTestConfig(t)
	cmd := newStatusCommand()
	cmd.SetArgs([]string{"--config", configPath})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext returned error: %v", err)
	}

	output := stdout.String()
	for _, want := range []string{"system_config_state: invalid", "system_config_path: " + systemPath} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestStatusCommandReportsDiscoveredAndEnabledModules(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	home := t.TempDir()
	t.Setenv("HOME", home)
	config.SetSystemKeyringGetForTest(func(service string, account string) (string, error) {
		return hex.EncodeToString([]byte("0123456789abcdef0123456789abcdef")), nil
	})
	t.Cleanup(func() { config.SetSystemKeyringGetForTest(keyring.Get) })

	projectRoot := t.TempDir()
	configPath := writeStatusConfigWithRoot(t, projectRoot)
	projectPath := filepath.Join(projectRoot, ".rillan", "project.yaml")
	if err := os.MkdirAll(filepath.Dir(projectPath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(projectPath, []byte("name: \"demo\"\nmodules:\n  enabled: [demo]\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	writeModuleManifest(t, filepath.Join(projectRoot, ".rillan", "modules", "demo", "module.yaml"), "id: \"demo\"\nversion: \"0.1.0\"\nentrypoint: [\"./bin/module\"]\n")
	writeModuleManifest(t, filepath.Join(projectRoot, ".rillan", "modules", "other", "module.yaml"), "id: \"other\"\nversion: \"0.1.0\"\nentrypoint: [\"./bin/module\"]\n")
	manifestData, err := os.ReadFile(filepath.Join(projectRoot, ".rillan", "modules", "demo", "module.yaml"))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	payload, err := encryptSystemPolicyPayloadForStatusTest(config.SystemPolicy{TrustedModules: []config.TrustedModulePolicy{{RepoRoot: projectRoot, ModuleID: "demo", ManifestSHA256: sha256Hex(manifestData)}}}, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("encryptSystemPolicyPayloadForStatusTest returned error: %v", err)
	}
	writeStatusSystemConfig(t, home, payload)

	cmd := newStatusCommand()
	cmd.SetArgs([]string{"--config", configPath})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext returned error: %v", err)
	}

	output := stdout.String()
	for _, want := range []string{"modules_discovered: 2", "modules_enabled: 1", "module_ids: demo"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestStatusCommandRejectsEnabledButUntrustedModule(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	home := t.TempDir()
	t.Setenv("HOME", home)
	config.SetSystemKeyringGetForTest(func(service string, account string) (string, error) {
		return hex.EncodeToString([]byte("0123456789abcdef0123456789abcdef")), nil
	})
	t.Cleanup(func() { config.SetSystemKeyringGetForTest(keyring.Get) })

	projectRoot := t.TempDir()
	configPath := writeStatusConfigWithRoot(t, projectRoot)
	projectPath := filepath.Join(projectRoot, ".rillan", "project.yaml")
	if err := os.MkdirAll(filepath.Dir(projectPath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(projectPath, []byte("name: \"demo\"\nmodules:\n  enabled: [demo]\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	writeModuleManifest(t, filepath.Join(projectRoot, ".rillan", "modules", "demo", "module.yaml"), "id: \"demo\"\nversion: \"0.1.0\"\nentrypoint: [\"./bin/module\"]\n")
	payload, err := encryptSystemPolicyPayloadForStatusTest(config.SystemPolicy{}, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("encryptSystemPolicyPayloadForStatusTest returned error: %v", err)
	}
	writeStatusSystemConfig(t, home, payload)

	cmd := newStatusCommand()
	cmd.SetArgs([]string{"--config", configPath})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)

	if err := cmd.ExecuteContext(context.Background()); err == nil {
		t.Fatal("expected status command to fail for untrusted enabled module")
	}
}

func writeStatusTestConfig(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `server:
  host: "127.0.0.1"
  port: 8420
  log_level: "info"

index:
  root: "/configured-root"

runtime:
  vector_store_mode: "embedded"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	return path
}

func writeLocalModelStatusConfig(t *testing.T, baseURL string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `server:
  host: "127.0.0.1"
  port: 8420
  log_level: "info"

index:
  root: "/configured-root"

local_model:
  enabled: true
  base_url: "` + baseURL + `"
  embed_model: "nomic-embed-text"
  query_rewrite:
    enabled: true
    model: "qwen3:0.6b"

runtime:
  vector_store_mode: "embedded"
  local_model_base_url: "` + baseURL + `"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	return path
}

func writeStatusConfigWithRoot(t *testing.T, root string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `server:
  host: "127.0.0.1"
  port: 8420
  log_level: "info"

index:
  root: "` + root + `"

runtime:
  vector_store_mode: "embedded"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	return path
}

func writeModuleManifest(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}

func writeStatusSystemConfig(t *testing.T, home string, payload string) {
	t.Helper()
	path := filepath.Join(home, ".rillan", "system.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	content := "encrypted_payload: \"" + payload + "\"\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}

func encryptSystemPolicyPayloadForStatusTest(policy config.SystemPolicy, key []byte) (string, error) {
	plaintext, err := json.Marshal(policy)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	combined := append(nonce, ciphertext...)
	return base64.StdEncoding.EncodeToString(combined), nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
