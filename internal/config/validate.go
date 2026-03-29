package config

import (
	"fmt"
	"strings"
)

func Validate(cfg Config) error {
	return ValidateForMode(cfg, ValidationModeServe)
}

func ValidateProject(cfg ProjectConfig) error {
	if strings.TrimSpace(cfg.Name) == "" {
		return fmt.Errorf("project.name must not be empty")
	}

	switch normalizeString(cfg.Classification) {
	case ProjectClassificationOpenSource, ProjectClassificationInternal, ProjectClassificationProprietary, ProjectClassificationTradeSecret:
	default:
		return fmt.Errorf("project.classification must be one of %q, %q, %q, or %q", ProjectClassificationOpenSource, ProjectClassificationInternal, ProjectClassificationProprietary, ProjectClassificationTradeSecret)
	}

	if err := validateRoutePreference("project.routing.default", cfg.Routing.Default); err != nil {
		return err
	}

	for i, source := range cfg.Sources {
		if strings.TrimSpace(source.Path) == "" {
			return fmt.Errorf("project.sources[%d].path must not be empty", i)
		}
		if strings.TrimSpace(source.Type) == "" {
			return fmt.Errorf("project.sources[%d].type must not be empty", i)
		}
	}

	for key, value := range cfg.Routing.TaskTypes {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("project.routing.task_types must not contain empty task names")
		}
		if err := validateRoutePreference(fmt.Sprintf("project.routing.task_types[%q]", key), value); err != nil {
			return err
		}
	}

	for i, instruction := range cfg.Instructions {
		if strings.TrimSpace(instruction) == "" {
			return fmt.Errorf("project.instructions[%d] must not be empty", i)
		}
	}

	return nil
}

func ValidateSystem(cfg SystemConfig) error {
	if strings.TrimSpace(cfg.Version) == "" {
		return fmt.Errorf("system.version must not be empty")
	}

	switch normalizeString(cfg.Encryption.Method) {
	case SystemEncryptionKeyringAESGCM:
	default:
		return fmt.Errorf("system.encryption.method must be %q", SystemEncryptionKeyringAESGCM)
	}

	if strings.TrimSpace(cfg.Encryption.KeyringService) == "" {
		return fmt.Errorf("system.encryption.keyring_service must not be empty")
	}
	if strings.TrimSpace(cfg.Encryption.KeyringAccount) == "" {
		return fmt.Errorf("system.encryption.keyring_account must not be empty")
	}
	if strings.TrimSpace(cfg.EncryptedPayload) == "" {
		return fmt.Errorf("system.encrypted_payload must not be empty")
	}

	return nil
}

func ValidateForMode(cfg Config, mode ValidationMode) error {
	if cfg.Server.Host == "" {
		return fmt.Errorf("server.host must not be empty")
	}
	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535")
	}

	switch normalizeString(cfg.Server.LogLevel) {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("server.log_level must be one of debug, info, warn, or error")
	}

	if normalizeString(cfg.Runtime.VectorStoreMode) != "embedded" {
		return fmt.Errorf("runtime.vector_store_mode must be %q in milestone two", "embedded")
	}

	if cfg.Index.ChunkSizeLines < 1 {
		return fmt.Errorf("index.chunk_size_lines must be greater than zero")
	}
	if cfg.Retrieval.TopK < 1 {
		return fmt.Errorf("retrieval.top_k must be greater than zero")
	}
	if cfg.Retrieval.MaxContextChars < 1 {
		return fmt.Errorf("retrieval.max_context_chars must be greater than zero")
	}
	for _, pattern := range cfg.Index.Includes {
		if strings.TrimSpace(pattern) == "" {
			return fmt.Errorf("index.includes must not contain empty patterns")
		}
	}
	for _, pattern := range cfg.Index.Excludes {
		if strings.TrimSpace(pattern) == "" {
			return fmt.Errorf("index.excludes must not contain empty patterns")
		}
	}

	if cfg.LocalModel.Enabled {
		if strings.TrimSpace(cfg.LocalModel.BaseURL) == "" {
			return fmt.Errorf("local_model.base_url must not be empty when local_model is enabled")
		}
		if strings.TrimSpace(cfg.LocalModel.EmbedModel) == "" {
			return fmt.Errorf("local_model.embed_model must not be empty when local_model is enabled")
		}
	}
	if cfg.LocalModel.QueryRewrite.Enabled {
		if !cfg.LocalModel.Enabled {
			return fmt.Errorf("local_model.enabled must be true when query_rewrite is enabled")
		}
		if strings.TrimSpace(cfg.LocalModel.QueryRewrite.Model) == "" {
			return fmt.Errorf("local_model.query_rewrite.model must not be empty when query_rewrite is enabled")
		}
	}

	if cfg.Agent.MCP.Enabled {
		if !cfg.Agent.MCP.ReadOnly {
			return fmt.Errorf("agent.mcp.read_only must be true in milestone seven")
		}
		if cfg.Agent.MCP.MaxOpenFiles < 1 {
			return fmt.Errorf("agent.mcp.max_open_files must be greater than zero when MCP is enabled")
		}
		if cfg.Agent.MCP.MaxDiagnostics < 1 {
			return fmt.Errorf("agent.mcp.max_diagnostics must be greater than zero when MCP is enabled")
		}
	}

	switch mode {
	case ValidationModeServe:
		switch normalizeString(cfg.Provider.Type) {
		case ProviderOpenAI:
			if cfg.Provider.OpenAI.APIKey == "" {
				return fmt.Errorf("provider.openai.api_key is required for the openai provider")
			}
		case ProviderAnthropic:
			if !cfg.Provider.Anthropic.Enabled {
				return fmt.Errorf("anthropic is never implicit; set provider.anthropic.enabled=true to opt in")
			}
			if cfg.Provider.Anthropic.APIKey == "" {
				return fmt.Errorf("provider.anthropic.api_key is required when anthropic is selected")
			}
		default:
			return fmt.Errorf("provider.type must be one of %q or %q", ProviderOpenAI, ProviderAnthropic)
		}
	case ValidationModeIndex:
		if strings.TrimSpace(cfg.Index.Root) == "" {
			return fmt.Errorf("index.root is required for the index command")
		}
	case ValidationModeStatus:
		return nil
	default:
		return fmt.Errorf("unsupported validation mode %q", mode)
	}

	return nil
}

func validateRoutePreference(field string, value string) error {
	switch normalizeString(value) {
	case RoutePreferenceAuto, RoutePreferencePreferLocal, RoutePreferencePreferCloud, RoutePreferenceLocalOnly:
		return nil
	default:
		return fmt.Errorf("%s must be one of %q, %q, %q, or %q", field, RoutePreferenceAuto, RoutePreferencePreferLocal, RoutePreferencePreferCloud, RoutePreferenceLocalOnly)
	}
}
