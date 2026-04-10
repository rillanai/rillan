// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

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

	"github.com/rillanai/rillan/internal/secretstore"
	"gopkg.in/yaml.v3"
)

type ValidationMode string

var ErrLLMProviderNotFound = errors.New("llm provider not found")

const (
	ValidationModeServe  ValidationMode = "serve"
	ValidationModeIndex  ValidationMode = "index"
	ValidationModeStatus ValidationMode = "status"
)

func Load(path string) (Config, error) {
	return LoadWithMode(path, ValidationModeServe)
}

func LoadForEdit(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg := DefaultConfig()
			applyDerivedDefaults(&cfg, path)
			return cfg, nil
		}
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}

	applyDerivedDefaults(&cfg, path)

	return cfg, nil
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

func ApplyEnvironmentOverrides(cfg *Config) {
	applyEnvOverrides(cfg)
}

func Write(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	output := cfg
	if output.SchemaVersion >= SchemaVersionV2 {
		output.Provider.OpenAI.APIKey = ""
		output.Provider.Anthropic.APIKey = ""
	}
	data, err := yaml.Marshal(output)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}

func ResolveActiveLLMProvider(cfg Config, project ProjectConfig) (ResolvedLLMProvider, error) {
	selectedID := strings.TrimSpace(cfg.LLMs.Default)
	if override := strings.TrimSpace(project.Providers.LLMDefault); override != "" {
		selectedID = override
	}
	if selectedID == "" {
		return ResolvedLLMProvider{}, fmt.Errorf("llms.default must not be empty")
	}
	if len(project.Providers.LLMAllowed) > 0 {
		allowed := false
		for _, candidate := range project.Providers.LLMAllowed {
			if strings.TrimSpace(candidate) == selectedID {
				allowed = true
				break
			}
		}
		if !allowed {
			return ResolvedLLMProvider{}, fmt.Errorf("llm provider %q is not allowed for this project", selectedID)
		}
	}
	for _, provider := range cfg.LLMs.Providers {
		if strings.TrimSpace(provider.ID) != selectedID {
			continue
		}
		return ResolvedLLMProvider{
			ID:            provider.ID,
			Preset:        strings.TrimSpace(provider.Preset),
			Backend:       strings.TrimSpace(provider.Backend),
			Transport:     strings.TrimSpace(provider.Transport),
			Endpoint:      strings.TrimSpace(provider.Endpoint),
			Command:       append([]string(nil), provider.Command...),
			AuthStrategy:  strings.TrimSpace(provider.AuthStrategy),
			DefaultModel:  strings.TrimSpace(provider.DefaultModel),
			ModelPins:     append([]string(nil), provider.ModelPins...),
			Capabilities:  append([]string(nil), provider.Capabilities...),
			CredentialRef: strings.TrimSpace(provider.CredentialRef),
		}, nil
	}
	return ResolvedLLMProvider{}, fmt.Errorf("llm provider %q not found", selectedID)
}

func ResolveRuntimeProviderConfig(cfg Config, project ProjectConfig) (ProviderConfig, error) {
	hostCfg, err := ResolveRuntimeProviderHostConfig(cfg, project)
	if err != nil {
		return ProviderConfig{}, err
	}
	for _, provider := range hostCfg.Providers {
		if provider.ID != hostCfg.Default {
			continue
		}
		return ProviderConfig{
			Type:      provider.Type,
			OpenAI:    provider.OpenAI,
			Anthropic: provider.Anthropic,
			Local:     provider.LocalModel,
		}, nil
	}
	return ProviderConfig{}, fmt.Errorf("default runtime provider %q not found", hostCfg.Default)
}

func ResolveLLMProviderByID(cfg Config, providerID string) (ResolvedLLMProvider, error) {
	selectedID := strings.TrimSpace(providerID)
	if cfg.SchemaVersion < SchemaVersionV2 || len(cfg.LLMs.Providers) == 0 {
		if selectedID == "" || selectedID == defaultRuntimeProviderID {
			return ResolvedLLMProvider{
				ID:           defaultRuntimeProviderID,
				Backend:      strings.TrimSpace(cfg.Provider.Type),
				Transport:    LLMTransportHTTP,
				Endpoint:     defaultLegacyProviderEndpoint(cfg),
				AuthStrategy: defaultLegacyProviderAuthStrategy(cfg),
			}, nil
		}
		return ResolvedLLMProvider{}, fmt.Errorf("%w: %q", ErrLLMProviderNotFound, providerID)
	}

	for _, provider := range cfg.LLMs.Providers {
		if strings.TrimSpace(provider.ID) != selectedID {
			continue
		}
		return ResolvedLLMProvider{
			ID:            provider.ID,
			Preset:        strings.TrimSpace(provider.Preset),
			Backend:       strings.TrimSpace(provider.Backend),
			Transport:     strings.TrimSpace(provider.Transport),
			Endpoint:      strings.TrimSpace(provider.Endpoint),
			Command:       append([]string(nil), provider.Command...),
			AuthStrategy:  strings.TrimSpace(provider.AuthStrategy),
			DefaultModel:  strings.TrimSpace(provider.DefaultModel),
			ModelPins:     append([]string(nil), provider.ModelPins...),
			Capabilities:  append([]string(nil), provider.Capabilities...),
			CredentialRef: strings.TrimSpace(provider.CredentialRef),
		}, nil
	}

	return ResolvedLLMProvider{}, fmt.Errorf("%w: %q", ErrLLMProviderNotFound, providerID)
}

func ResolveRuntimeProviderHostConfig(cfg Config, project ProjectConfig) (RuntimeProviderHostConfig, error) {
	if cfg.SchemaVersion < SchemaVersionV2 || len(cfg.LLMs.Providers) == 0 {
		return RuntimeProviderHostConfig{
			Default: defaultRuntimeProviderID,
			Providers: []RuntimeProviderAdapterConfig{{
				ID:         defaultRuntimeProviderID,
				Type:       cfg.Provider.Type,
				OpenAI:     cfg.Provider.OpenAI,
				Anthropic:  cfg.Provider.Anthropic,
				LocalModel: cfg.Provider.Local,
			}},
		}, nil
	}
	selected, err := ResolveActiveLLMProvider(cfg, project)
	if err != nil {
		return RuntimeProviderHostConfig{}, err
	}

	allowed := makeAllowlist(project.Providers.LLMAllowed)
	providers := make([]RuntimeProviderAdapterConfig, 0, len(cfg.LLMs.Providers))
	for _, provider := range cfg.LLMs.Providers {
		providerID := strings.TrimSpace(provider.ID)
		if providerID == "" {
			continue
		}
		if len(allowed) > 0 {
			if _, ok := allowed[providerID]; !ok {
				continue
			}
		}

		resolved, err := ResolveLLMProviderByID(cfg, providerID)
		if err != nil {
			if providerID == selected.ID {
				return RuntimeProviderHostConfig{}, err
			}
			continue
		}
		providerCfg, err := ResolveRuntimeProviderAdapterConfig(cfg, resolved)
		if err != nil {
			if providerID == selected.ID {
				return RuntimeProviderHostConfig{}, err
			}
			continue
		}
		providers = append(providers, providerCfg)
	}
	if len(providers) == 0 {
		return RuntimeProviderHostConfig{}, fmt.Errorf("runtime provider host must include at least one provider")
	}
	return RuntimeProviderHostConfig{
		Default:   selected.ID,
		Providers: providers,
	}, nil
}

func ResolveServerAuthBearer(cfg Config) (string, error) {
	return secretstore.ResolveBearer(cfg.Server.Auth.SessionRef, secretstore.Binding{AuthStrategy: strings.TrimSpace(cfg.Server.Auth.AuthStrategy)})
}

func ResolveRuntimeProviderAdapterConfig(cfg Config, selected ResolvedLLMProvider) (RuntimeProviderAdapterConfig, error) {
	providerCfg := RuntimeProviderAdapterConfig{
		ID:        selected.ID,
		Preset:    selected.Preset,
		Type:      selected.Backend,
		Transport: selected.Transport,
		Command:   append([]string(nil), selected.Command...),
	}

	if selected.Transport == LLMTransportSTDIO {
		if len(selected.Command) == 0 {
			return RuntimeProviderAdapterConfig{}, fmt.Errorf("llm provider %q command must not be empty when transport is %q", selected.ID, LLMTransportSTDIO)
		}
		if strategy := normalizeString(selected.AuthStrategy); strategy != "" && strategy != AuthStrategyNone {
			return RuntimeProviderAdapterConfig{}, fmt.Errorf("llm provider %q uses unsupported auth strategy %q for stdio transport", selected.ID, selected.AuthStrategy)
		}
		return providerCfg, nil
	}

	switch selected.Backend {
	case LLMBackendOpenAICompatible:
		secret, err := resolveRuntimeProviderBearer(selected)
		if err != nil {
			return RuntimeProviderAdapterConfig{}, err
		}
		providerCfg.OpenAI = OpenAIConfig{
			BaseURL: selected.Endpoint,
			APIKey:  secret,
		}
		return providerCfg, nil
	case ProviderAnthropic:
		apiKey, err := resolveRuntimeProviderAPIKey(selected)
		if err != nil {
			return RuntimeProviderAdapterConfig{}, err
		}
		providerCfg.Anthropic = AnthropicConfig{
			Enabled: true,
			BaseURL: selected.Endpoint,
			APIKey:  apiKey,
		}
		return providerCfg, nil
	case ProviderOllama:
		baseURL := strings.TrimSpace(selected.Endpoint)
		if baseURL == "" {
			baseURL = strings.TrimSpace(cfg.LocalModel.BaseURL)
		}
		if baseURL == "" {
			return RuntimeProviderAdapterConfig{}, fmt.Errorf("llm provider %q endpoint must not be empty for ollama", selected.ID)
		}
		providerCfg.LocalModel = LocalModelProvider{BaseURL: baseURL}
		return providerCfg, nil
	default:
		return RuntimeProviderAdapterConfig{}, fmt.Errorf("llm provider %q uses unsupported backend %q in the current runtime", selected.ID, selected.Backend)
	}
}

func makeAllowlist(values []string) map[string]struct{} {
	allowed := make(map[string]struct{}, len(values))
	for _, value := range values {
		candidateID := strings.TrimSpace(value)
		if candidateID == "" {
			continue
		}
		allowed[candidateID] = struct{}{}
	}
	return allowed
}

func resolveRuntimeProviderBearer(selected ResolvedLLMProvider) (string, error) {
	if selected.AuthStrategy == AuthStrategyNone || selected.CredentialRef == "" {
		return "", nil
	}
	binding := secretstore.Binding{Endpoint: selected.Endpoint, AuthStrategy: selected.AuthStrategy}
	resolved, err := secretstore.ResolveBearer(selected.CredentialRef, binding)
	if err != nil {
		return "", err
	}
	return resolved, nil
}

func resolveRuntimeProviderAPIKey(selected ResolvedLLMProvider) (string, error) {
	if selected.AuthStrategy != AuthStrategyAPIKey {
		return "", fmt.Errorf("llm provider %q uses unsupported auth strategy %q for anthropic; only %q is supported", selected.ID, selected.AuthStrategy, AuthStrategyAPIKey)
	}
	if strings.TrimSpace(selected.CredentialRef) == "" {
		return "", fmt.Errorf("llm provider %q must include credential_ref when auth_strategy is %q", selected.ID, AuthStrategyAPIKey)
	}
	credential, err := secretstore.Load(selected.CredentialRef)
	if err != nil {
		return "", err
	}
	binding := secretstore.Binding{Endpoint: selected.Endpoint, AuthStrategy: selected.AuthStrategy}
	if err := validateRuntimeProviderCredentialBinding(credential, binding); err != nil {
		return "", err
	}
	if strings.TrimSpace(credential.APIKey) == "" {
		return "", fmt.Errorf("credential at %s does not contain an api key", selected.CredentialRef)
	}
	return credential.APIKey, nil
}

func validateRuntimeProviderCredentialBinding(credential secretstore.Credential, binding secretstore.Binding) error {
	if binding.Endpoint != "" && credential.Endpoint != "" && credential.Endpoint != binding.Endpoint {
		return fmt.Errorf("stored credential endpoint %q does not match %q", credential.Endpoint, binding.Endpoint)
	}
	if binding.AuthStrategy != "" && credential.AuthStrategy != "" && credential.AuthStrategy != binding.AuthStrategy {
		return fmt.Errorf("stored credential auth strategy %q does not match %q", credential.AuthStrategy, binding.AuthStrategy)
	}
	if binding.Issuer != "" && credential.Issuer != "" && credential.Issuer != binding.Issuer {
		return fmt.Errorf("stored credential issuer %q does not match %q", credential.Issuer, binding.Issuer)
	}
	if binding.Audience != "" && credential.Audience != "" && credential.Audience != binding.Audience {
		return fmt.Errorf("stored credential audience %q does not match %q", credential.Audience, binding.Audience)
	}
	return nil
}

const defaultRuntimeProviderID = "default"

func defaultLegacyProviderEndpoint(cfg Config) string {
	switch normalizeString(cfg.Provider.Type) {
	case ProviderOpenAI:
		return strings.TrimSpace(cfg.Provider.OpenAI.BaseURL)
	case ProviderAnthropic:
		return strings.TrimSpace(cfg.Provider.Anthropic.BaseURL)
	default:
		return strings.TrimSpace(cfg.Provider.Local.BaseURL)
	}
}

func defaultLegacyProviderAuthStrategy(cfg Config) string {
	switch normalizeString(cfg.Provider.Type) {
	case ProviderOpenAI, ProviderAnthropic:
		return AuthStrategyAPIKey
	default:
		return AuthStrategyNone
	}
}

func applyDerivedDefaults(cfg *Config, configPath string) {
	if cfg.SchemaVersion == 0 {
		cfg.SchemaVersion = SchemaVersionV2
	}
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
	if cfg.Agent.ApprovedRepoRoots == nil {
		cfg.Agent.ApprovedRepoRoots = []string{}
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
	for i := range cfg.Agent.ApprovedRepoRoots {
		cfg.Agent.ApprovedRepoRoots[i] = resolveProjectPath(configPath, cfg.Agent.ApprovedRepoRoots[i])
	}
	if cfg.LLMs.Providers == nil {
		cfg.LLMs.Providers = slices.Clone(DefaultConfig().LLMs.Providers)
	}
	for i := range cfg.LLMs.Providers {
		applyLLMProviderPresetDefaults(&cfg.LLMs.Providers[i])
	}
	if cfg.LLMs.Default == "" && len(cfg.LLMs.Providers) > 0 {
		cfg.LLMs.Default = cfg.LLMs.Providers[0].ID
	}
	if cfg.MCPs.Servers == nil {
		cfg.MCPs.Servers = []MCPServerConfig{}
	}
}

func applyLLMProviderPresetDefaults(provider *LLMProviderConfig) {
	presetID := normalizeString(provider.Preset)
	if presetID == "" {
		return
	}
	preset := BundledLLMProviderPreset(presetID)
	if preset.ID == "" {
		return
	}
	if strings.TrimSpace(provider.Backend) == "" {
		provider.Backend = preset.Family
	}
	if strings.TrimSpace(provider.Transport) == "" {
		provider.Transport = LLMTransportHTTP
	}
	if strings.TrimSpace(provider.Endpoint) == "" {
		provider.Endpoint = preset.Endpoint
	}
	if strings.TrimSpace(provider.AuthStrategy) == "" {
		provider.AuthStrategy = preset.AuthStrategy
	}
	if strings.TrimSpace(provider.DefaultModel) == "" {
		provider.DefaultModel = preset.DefaultModel
	}
	if len(provider.ModelPins) == 0 && len(preset.ModelPins) > 0 {
		provider.ModelPins = append([]string(nil), preset.ModelPins...)
	}
	if len(provider.Capabilities) == 0 {
		provider.Capabilities = append([]string(nil), preset.Capabilities...)
	}
	if strings.TrimSpace(provider.CredentialRef) == "" && strings.TrimSpace(provider.ID) != "" {
		provider.CredentialRef = "keyring://rillan/llm/" + strings.TrimSpace(provider.ID)
	}
	if len(provider.ModelPins) == 0 && strings.TrimSpace(provider.DefaultModel) != "" {
		provider.ModelPins = []string{strings.TrimSpace(provider.DefaultModel)}
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
	if cfg.Providers.LLMAllowed == nil {
		cfg.Providers.LLMAllowed = []string{}
	}
	if cfg.Providers.MCPEnabled == nil {
		cfg.Providers.MCPEnabled = []string{}
	}
	if cfg.Modules.Enabled == nil {
		cfg.Modules.Enabled = []string{}
	}
	if cfg.Agent.Skills.Enabled == nil {
		cfg.Agent.Skills.Enabled = []string{}
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
	applyBoolEnv(&cfg.Server.AllowNonLoopbackBind, "RILLAN_SERVER_ALLOW_NON_LOOPBACK_BIND")
	applyBoolEnv(&cfg.Server.Auth.Enabled, "RILLAN_SERVER_AUTH_ENABLED")
	applyStringEnv(&cfg.Server.Auth.AuthStrategy, "RILLAN_SERVER_AUTH_STRATEGY")
	applyStringEnv(&cfg.Server.Auth.SessionRef, "RILLAN_SERVER_AUTH_SESSION_REF")

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
			return filepath.Join(".rillan", "project.yaml")
		}
		base = cwd
	}

	if absBase, err := filepath.Abs(base); err == nil {
		base = absBase
	}

	return filepath.Join(base, ".rillan", "project.yaml")
}

func LegacyProjectConfigPath(root string) string {
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

func ResolveProjectConfigPath(root string) string {
	preferred := DefaultProjectConfigPath(root)
	if _, err := os.Stat(preferred); err == nil {
		return preferred
	}
	legacy := LegacyProjectConfigPath(root)
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	return preferred
}

func DefaultSystemConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".rillan", "system.yaml")
	}
	return filepath.Join(home, ".rillan", "system.yaml")
}

func LegacySystemConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".sidekick", "system.yaml")
	}
	return filepath.Join(home, ".sidekick", "system.yaml")
}

func ResolveSystemConfigPath() string {
	preferred := DefaultSystemConfigPath()
	if _, err := os.Stat(preferred); err == nil {
		return preferred
	}
	legacy := LegacySystemConfigPath()
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	return preferred
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
