package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sidekickos/rillan/internal/config"
	"github.com/sidekickos/rillan/internal/secretstore"
)

func TestMCPAddCreatesServerEntry(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	cmd := newMCPCommand()
	cmd.SetArgs([]string{"--config", configPath, "add", "ide-local", "--endpoint", "http://127.0.0.1:8765", "--transport", "http", "--auth-strategy", "none"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	cfg, err := config.LoadForEdit(configPath)
	if err != nil {
		t.Fatalf("LoadForEdit returned error: %v", err)
	}
	if got, want := cfg.MCPs.Default, "ide-local"; got != want {
		t.Fatalf("mcps.default = %q, want %q", got, want)
	}
	if got, want := len(cfg.MCPs.Servers), 1; got != want {
		t.Fatalf("len(mcps.servers) = %d, want %d", got, want)
	}
	if !cfg.MCPs.Servers[0].ReadOnly {
		t.Fatal("expected read_only to default to true")
	}
}

func TestMCPAddSupportsStdioTransportWithCommand(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	cmd := newMCPCommand()
	cmd.SetArgs([]string{"--config", configPath, "add", "repo-plugin", "--transport", "stdio", "--command", "rillan-mcp-demo", "--auth-strategy", "none"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	cfg, err := config.LoadForEdit(configPath)
	if err != nil {
		t.Fatalf("LoadForEdit returned error: %v", err)
	}
	if got, want := cfg.MCPs.Servers[0].Transport, "stdio"; got != want {
		t.Fatalf("transport = %q, want %q", got, want)
	}
	if got, want := strings.Join(cfg.MCPs.Servers[0].Command, ","), "rillan-mcp-demo"; got != want {
		t.Fatalf("command = %q, want %q", got, want)
	}
}

func TestMCPUseSwitchesDefaultServer(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.DefaultConfig()
	cfg.MCPs.Servers = []config.MCPServerConfig{
		{ID: "ide-local", Endpoint: "http://127.0.0.1:8765", Transport: "http", AuthStrategy: config.AuthStrategyNone, ReadOnly: true},
		{ID: "repo-gateway", Endpoint: "http://127.0.0.1:8766", Transport: "http", AuthStrategy: config.AuthStrategyAPIKey, ReadOnly: true},
	}
	if err := config.Write(configPath, cfg); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	cmd := newMCPCommand()
	cmd.SetArgs([]string{"--config", configPath, "use", "repo-gateway"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	reloaded, err := config.LoadForEdit(configPath)
	if err != nil {
		t.Fatalf("LoadForEdit returned error: %v", err)
	}
	if got, want := reloaded.MCPs.Default, "repo-gateway"; got != want {
		t.Fatalf("mcps.default = %q, want %q", got, want)
	}
}

func TestMCPRemoveDeletesServerEntry(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.DefaultConfig()
	cfg.MCPs.Default = "ide-local"
	cfg.MCPs.Servers = []config.MCPServerConfig{{ID: "ide-local", Endpoint: "http://127.0.0.1:8765", Transport: "http", AuthStrategy: config.AuthStrategyNone, ReadOnly: true}}
	if err := config.Write(configPath, cfg); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	cmd := newMCPCommand()
	cmd.SetArgs([]string{"--config", configPath, "remove", "ide-local"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	reloaded, err := config.LoadForEdit(configPath)
	if err != nil {
		t.Fatalf("LoadForEdit returned error: %v", err)
	}
	if got := len(reloaded.MCPs.Servers); got != 0 {
		t.Fatalf("len(mcps.servers) = %d, want 0", got)
	}
	if reloaded.MCPs.Default != "" {
		t.Fatalf("mcps.default = %q, want empty", reloaded.MCPs.Default)
	}
}

func TestMCPListPrintsSortedServers(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.DefaultConfig()
	cfg.MCPs.Default = "ide-local"
	cfg.MCPs.Servers = []config.MCPServerConfig{
		{ID: "repo-gateway", Endpoint: "http://127.0.0.1:8766", Transport: "http", AuthStrategy: config.AuthStrategyAPIKey, ReadOnly: true},
		{ID: "ide-local", Endpoint: "http://127.0.0.1:8765", Transport: "http", AuthStrategy: config.AuthStrategyNone, ReadOnly: true},
	}
	if err := config.Write(configPath, cfg); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	cmd := newMCPCommand()
	cmd.SetArgs([]string{"--config", configPath, "list"})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "default: ide-local") {
		t.Fatalf("output missing default:\n%s", output)
	}
	if strings.Index(output, "- id: ide-local") > strings.Index(output, "- id: repo-gateway") {
		t.Fatalf("servers not sorted by id:\n%s", output)
	}
}

func TestMCPLoginAndLogoutStoreCredentialsSecurely(t *testing.T) {
	store := map[string]string{}
	secretstoreTestHooks(t, store)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.DefaultConfig()
	cfg.MCPs.Servers = []config.MCPServerConfig{{ID: "ide-local", Endpoint: "http://127.0.0.1:8765", Transport: "http", AuthStrategy: config.AuthStrategyAPIKey, ReadOnly: true, CredentialRef: credentialRefForMCP("ide-local")}}
	if err := config.Write(configPath, cfg); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	login := newMCPCommand()
	login.SetArgs([]string{"--config", configPath, "login", "ide-local", "--api-key", "secret-key"})
	if err := login.Execute(); err != nil {
		t.Fatalf("login Execute returned error: %v", err)
	}
	if _, ok := store[fmt.Sprintf("rillan/mcp/%s", "ide-local")]; !ok {
		t.Fatal("expected keyring entry to be created")
	}
	if !secretstore.Exists(credentialRefForMCP("ide-local")) {
		t.Fatal("expected mcp credential ref to resolve")
	}

	logout := newMCPCommand()
	logout.SetArgs([]string{"--config", configPath, "logout", "ide-local"})
	if err := logout.Execute(); err != nil {
		t.Fatalf("logout Execute returned error: %v", err)
	}
	if secretstore.Exists(credentialRefForMCP("ide-local")) {
		t.Fatal("expected mcp credential ref to be cleared")
	}
}
