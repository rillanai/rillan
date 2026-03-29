package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type ValidationMode string

const (
	ValidationModeServe  ValidationMode = "serve"
	ValidationModeIndex  ValidationMode = "index"
	ValidationModeStatus ValidationMode = "status"
)

func Load(path string) (Config, error) {
	return LoadWithMode(path, ValidationModeServe)
}

func LoadProject(path string) (ProjectConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ProjectConfig{}, fmt.Errorf("read project config %s: %w", path, err)
	}

	cfg := DefaultProjectConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return ProjectConfig{}, fmt.Errorf("parse project config: %w", err)
	}

	applyProjectDerivedDefaults(&cfg, path)

	if err := ValidateProject(cfg); err != nil {
		return ProjectConfig{}, err
	}

	return cfg, nil
}

func LoadSystem(path string) (SystemConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SystemConfig{}, fmt.Errorf("read system config %s: %w", path, err)
	}

	if err := rejectPlaintextSystemConfig(data); err != nil {
		return SystemConfig{}, err
	}

	cfg := DefaultSystemConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return SystemConfig{}, fmt.Errorf("parse system config: %w", err)
	}

	applySystemDerivedDefaults(&cfg)

	if err := ValidateSystem(cfg); err != nil {
		return SystemConfig{}, err
	}
	if err := decryptSystemPolicy(&cfg); err != nil {
		return SystemConfig{}, err
	}

	return cfg, nil
}

func LoadWithMode(path string, mode ValidationMode) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, fmt.Errorf("config file not found at %s; run `rillan init --output %s` first", path, path)
		}
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}

	applyEnvOverrides(&cfg)
	applyDerivedDefaults(&cfg, path)

	if err := ValidateForMode(cfg, mode); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func applyDerivedDefaults(cfg *Config, configPath string) {
	if cfg.Provider.Type == "" {
		cfg.Provider.Type = ProviderOpenAI
	}
	if cfg.Provider.OpenAI.BaseURL == "" {
		cfg.Provider.OpenAI.BaseURL = "https://api.openai.com/v1"
	}
	if cfg.Provider.Anthropic.BaseURL == "" {
		cfg.Provider.Anthropic.BaseURL = "https://api.anthropic.com"
	}
	if cfg.Provider.Local.BaseURL == "" {
		cfg.Provider.Local.BaseURL = "http://127.0.0.1:11434"
	}
	if cfg.Runtime.LocalModelBaseURL == "" {
		cfg.Runtime.LocalModelBaseURL = cfg.Provider.Local.BaseURL
	}
	if cfg.Runtime.VectorStoreMode == "" {
		cfg.Runtime.VectorStoreMode = "embedded"
	}
	if len(cfg.Index.Excludes) == 0 {
		cfg.Index.Excludes = slices.Clone(DefaultConfig().Index.Excludes)
	}
	if cfg.Index.ChunkSizeLines == 0 {
		cfg.Index.ChunkSizeLines = DefaultConfig().Index.ChunkSizeLines
	}
	if cfg.Retrieval.TopK == 0 {
		cfg.Retrieval.TopK = DefaultConfig().Retrieval.TopK
	}
	if cfg.Retrieval.MaxContextChars == 0 {
		cfg.Retrieval.MaxContextChars = DefaultConfig().Retrieval.MaxContextChars
	}
	if cfg.Server.Host == "" {
		cfg.Server.Host = "127.0.0.1"
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8420
	}
	if cfg.Server.LogLevel == "" {
		cfg.Server.LogLevel = "info"
	}
	if cfg.LocalModel.BaseURL == "" {
		cfg.LocalModel.BaseURL = "http://127.0.0.1:11434"
	}
	if cfg.LocalModel.EmbedModel == "" {
		cfg.LocalModel.EmbedModel = "nomic-embed-text"
	}
	if cfg.LocalModel.QueryRewrite.Model == "" {
		cfg.LocalModel.QueryRewrite.Model = "qwen3:0.6b"
	}
	if cfg.Agent.MCP.MaxOpenFiles == 0 {
		cfg.Agent.MCP.MaxOpenFiles = DefaultConfig().Agent.MCP.MaxOpenFiles
	}
	if cfg.Agent.MCP.MaxDiagnostics == 0 {
		cfg.Agent.MCP.MaxDiagnostics = DefaultConfig().Agent.MCP.MaxDiagnostics
	}
	if !cfg.Agent.MCP.Enabled {
		cfg.Agent.MCP.ReadOnly = DefaultConfig().Agent.MCP.ReadOnly
	}
	if cfg.Index.Root != "" {
		cfg.Index.Root = resolveIndexRoot(configPath, cfg.Index.Root)
	}
}

func applyProjectDerivedDefaults(cfg *ProjectConfig, projectPath string) {
	if cfg.Classification == "" {
		cfg.Classification = DefaultProjectConfig().Classification
	}
	if cfg.Routing.Default == "" {
		cfg.Routing.Default = DefaultProjectConfig().Routing.Default
	}
	if cfg.Routing.TaskTypes == nil {
		cfg.Routing.TaskTypes = map[string]string{}
	}
	if cfg.Sources == nil {
		cfg.Sources = []ProjectSource{}
	}
	if cfg.Instructions == nil {
		cfg.Instructions = []string{}
	}

	for i := range cfg.Sources {
		cfg.Sources[i].Path = resolveProjectPath(projectPath, cfg.Sources[i].Path)
	}
}

func applySystemDerivedDefaults(cfg *SystemConfig) {
	if cfg.Version == "" {
		cfg.Version = DefaultSystemConfig().Version
	}
	if cfg.Encryption.Method == "" {
		cfg.Encryption.Method = DefaultSystemConfig().Encryption.Method
	}
	if cfg.Encryption.KeyringService == "" {
		cfg.Encryption.KeyringService = DefaultSystemConfig().Encryption.KeyringService
	}
	if cfg.Encryption.KeyringAccount == "" {
		cfg.Encryption.KeyringAccount = DefaultSystemConfig().Encryption.KeyringAccount
	}
}

func applyEnvOverrides(cfg *Config) {
	applyStringEnv(&cfg.Server.Host, "RILLAN_SERVER_HOST")
	applyIntEnv(&cfg.Server.Port, "RILLAN_SERVER_PORT")
	applyStringEnv(&cfg.Server.LogLevel, "RILLAN_SERVER_LOG_LEVEL")

	applyStringEnv(&cfg.Provider.Type, "RILLAN_PROVIDER_TYPE")
	applyStringEnv(&cfg.Provider.OpenAI.BaseURL, "RILLAN_OPENAI_BASE_URL")
	applyStringEnv(&cfg.Provider.OpenAI.APIKey, "RILLAN_OPENAI_API_KEY", "OPENAI_API_KEY")
	applyBoolEnv(&cfg.Provider.Anthropic.Enabled, "RILLAN_ANTHROPIC_ENABLED")
	applyStringEnv(&cfg.Provider.Anthropic.BaseURL, "RILLAN_ANTHROPIC_BASE_URL")
	applyStringEnv(&cfg.Provider.Anthropic.APIKey, "RILLAN_ANTHROPIC_API_KEY", "ANTHROPIC_API_KEY")
	applyStringEnv(&cfg.Provider.Local.BaseURL, "RILLAN_LOCAL_MODEL_BASE_URL")
	applyStringEnv(&cfg.Index.Root, "RILLAN_INDEX_ROOT")
	applyCSVEnv(&cfg.Index.Includes, "RILLAN_INDEX_INCLUDES")
	applyCSVEnv(&cfg.Index.Excludes, "RILLAN_INDEX_EXCLUDES")
	applyIntEnv(&cfg.Index.ChunkSizeLines, "RILLAN_INDEX_CHUNK_SIZE_LINES")
	applyBoolEnv(&cfg.Retrieval.Enabled, "RILLAN_RETRIEVAL_ENABLED")
	applyIntEnv(&cfg.Retrieval.TopK, "RILLAN_RETRIEVAL_TOP_K")
	applyIntEnv(&cfg.Retrieval.MaxContextChars, "RILLAN_RETRIEVAL_MAX_CONTEXT_CHARS")

	applyStringEnv(&cfg.Runtime.VectorStoreMode, "RILLAN_VECTOR_STORE_MODE")
	applyStringEnv(&cfg.Runtime.LocalModelBaseURL, "RILLAN_LOCAL_MODEL_BASE_URL")

	applyBoolEnv(&cfg.LocalModel.Enabled, "RILLAN_LOCAL_MODEL_ENABLED")
	applyStringEnv(&cfg.LocalModel.BaseURL, "RILLAN_LOCAL_MODEL_BASE_URL")
	applyStringEnv(&cfg.LocalModel.EmbedModel, "RILLAN_LOCAL_MODEL_EMBED_MODEL")
	applyBoolEnv(&cfg.LocalModel.QueryRewrite.Enabled, "RILLAN_LOCAL_MODEL_QUERY_REWRITE_ENABLED")
	applyStringEnv(&cfg.LocalModel.QueryRewrite.Model, "RILLAN_LOCAL_MODEL_QUERY_REWRITE_MODEL")

	applyBoolEnv(&cfg.Agent.Enabled, "RILLAN_AGENT_ENABLED")
	applyBoolEnv(&cfg.Agent.MCP.Enabled, "RILLAN_AGENT_MCP_ENABLED")
	applyBoolEnv(&cfg.Agent.MCP.ReadOnly, "RILLAN_AGENT_MCP_READ_ONLY")
	applyIntEnv(&cfg.Agent.MCP.MaxOpenFiles, "RILLAN_AGENT_MCP_MAX_OPEN_FILES")
	applyIntEnv(&cfg.Agent.MCP.MaxDiagnostics, "RILLAN_AGENT_MCP_MAX_DIAGNOSTICS")
}

func applyStringEnv(target *string, keys ...string) {
	for _, key := range keys {
		if value, ok := os.LookupEnv(key); ok {
			*target = value
			return
		}
	}
}

func applyIntEnv(target *int, key string) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return
	}

	parsed, err := strconv.Atoi(value)
	if err == nil {
		*target = parsed
	}
}

func applyBoolEnv(target *bool, key string) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return
	}

	parsed, err := strconv.ParseBool(value)
	if err == nil {
		*target = parsed
	}
}

func applyCSVEnv(target *[]string, key string) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return
	}

	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			items = append(items, trimmed)
		}
	}
	*target = items
}

func DefaultConfigPath() string {
	base, err := os.UserConfigDir()
	if err != nil {
		return "rillan.yaml"
	}
	return filepath.Join(base, "rillan", "config.yaml")
}

func DefaultProjectConfigPath(root string) string {
	base := strings.TrimSpace(root)
	if base == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return filepath.Join(".sidekick", "project.yaml")
		}
		base = cwd
	}

	if absBase, err := filepath.Abs(base); err == nil {
		base = absBase
	}

	return filepath.Join(base, ".sidekick", "project.yaml")
}

func DefaultSystemConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".sidekick", "system.yaml")
	}
	return filepath.Join(home, ".sidekick", "system.yaml")
}

func DefaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".rillan")
	}

	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "rillan", "data")
	}

	if base, ok := os.LookupEnv("XDG_DATA_HOME"); ok && base != "" {
		return filepath.Join(base, "rillan")
	}

	return filepath.Join(home, ".local", "share", "rillan")
}

func DefaultLogDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".rillan", "logs")
	}

	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Logs", "rillan")
	}

	if base, ok := os.LookupEnv("XDG_STATE_HOME"); ok && base != "" {
		return filepath.Join(base, "rillan", "logs")
	}

	return filepath.Join(home, ".local", "state", "rillan", "logs")
}

func normalizeString(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func resolveIndexRoot(configPath string, root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}

	if filepath.IsAbs(root) {
		return filepath.Clean(root)
	}

	baseDir := "."
	if configPath != "" {
		baseDir = filepath.Dir(configPath)
	}

	resolved := filepath.Join(baseDir, root)
	if absResolved, err := filepath.Abs(resolved); err == nil {
		return absResolved
	}

	return filepath.Clean(resolved)
}

func resolveProjectPath(projectPath string, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}

	baseDir := "."
	if projectPath != "" {
		baseDir = filepath.Dir(projectPath)
	}

	resolved := filepath.Join(baseDir, value)
	if absResolved, err := filepath.Abs(resolved); err == nil {
		return absResolved
	}

	return filepath.Clean(resolved)
}

func rejectPlaintextSystemConfig(data []byte) error {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse system config envelope: %w", err)
	}

	for _, key := range []string{"identity", "rules", "policy"} {
		if _, ok := raw[key]; ok {
			return fmt.Errorf("system config must not contain plaintext %q data; store only encrypted_payload and keyring metadata", key)
		}
	}

	return nil
}
