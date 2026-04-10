// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rillanai/rillan/internal/config"
	"github.com/rillanai/rillan/internal/secretstore"
	"github.com/spf13/cobra"
)

func newLLMCommand() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "llm",
		Short: "Manage named LLM providers and endpoint-bound auth",
	}

	cmd.PersistentFlags().StringVar(&configPath, "config", config.DefaultConfigPath(), "Path to the runtime config file")

	cmd.AddCommand(newLLMAddCommand(&configPath))
	cmd.AddCommand(newLLMRemoveCommand(&configPath))
	cmd.AddCommand(newLLMListCommand(&configPath))
	cmd.AddCommand(newLLMUseCommand(&configPath))
	cmd.AddCommand(newLLMLoginCommand(&configPath))
	cmd.AddCommand(newLLMLogoutCommand(&configPath))

	return cmd
}

func newLLMAddCommand(configPath *string) *cobra.Command {
	var preset string
	var backend string
	var transport string
	var endpoint string
	var command []string
	var authStrategy string
	var defaultModel string
	var capabilities []string

	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a named LLM provider entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			entry, err := buildLLMProviderEntry(strings.TrimSpace(args[0]), preset, backend, transport, endpoint, command, authStrategy, defaultModel, capabilities)
			if err != nil {
				return err
			}
			if err := validateLLMProviderEntry(entry); err != nil {
				return err
			}

			cfg, err := config.LoadForEdit(*configPath)
			if err != nil {
				return err
			}
			for _, existing := range cfg.LLMs.Providers {
				if existing.ID == entry.ID {
					return fmt.Errorf("llm provider %q already exists", entry.ID)
				}
			}
			cfg.LLMs.Providers = append(cfg.LLMs.Providers, entry)
			if cfg.LLMs.Default == "" {
				cfg.LLMs.Default = entry.ID
			}
			if err := config.Write(*configPath, cfg); err != nil {
				return err
			}
			return refreshDaemonAfterMutation(cfg, "updated llm provider config")
		},
	}

	cmd.Flags().StringVar(&preset, "preset", "", "Bundled provider preset (openai, xai, deepseek, kimi, zai)")
	cmd.Flags().StringVar(&backend, "backend", "", "Provider backend identity (for example openai_compatible)")
	cmd.Flags().StringVar(&transport, "transport", config.LLMTransportHTTP, "Provider transport (http or stdio)")
	cmd.Flags().StringVar(&endpoint, "endpoint", "", "Provider endpoint URL")
	cmd.Flags().StringSliceVar(&command, "command", nil, "Provider command for stdio transport")
	cmd.Flags().StringVar(&authStrategy, "auth-strategy", "", "Auth strategy (none, api_key, browser_oidc, device_oidc)")
	cmd.Flags().StringVar(&defaultModel, "default-model", "", "Default model name for this provider")
	cmd.Flags().StringSliceVar(&capabilities, "capability", nil, "Capability exposed by this provider (repeatable)")

	return cmd
}

func newLLMRemoveCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a named LLM provider entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			cfg, err := config.LoadForEdit(*configPath)
			if err != nil {
				return err
			}

			providers := make([]config.LLMProviderConfig, 0, len(cfg.LLMs.Providers))
			removed := false
			for _, provider := range cfg.LLMs.Providers {
				if provider.ID == id {
					removed = true
					continue
				}
				providers = append(providers, provider)
			}
			if !removed {
				return fmt.Errorf("llm provider %q not found", id)
			}
			cfg.LLMs.Providers = providers
			if cfg.LLMs.Default == id {
				cfg.LLMs.Default = ""
			}
			if err := config.Write(*configPath, cfg); err != nil {
				return err
			}
			return refreshDaemonAfterMutation(cfg, "updated llm provider config")
		},
	}
}

func newLLMListCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured LLM provider entries",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadForEdit(*configPath)
			if err != nil {
				return err
			}
			providers := append([]config.LLMProviderConfig(nil), cfg.LLMs.Providers...)
			sort.Slice(providers, func(i, j int) bool { return providers[i].ID < providers[j].ID })

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "default: %s\n", cfg.LLMs.Default)
			for _, provider := range providers {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "- id: %s\n  backend: %s\n  transport: %s\n  endpoint: %s\n  auth_strategy: %s\n  default_model: %s\n", provider.ID, provider.Backend, provider.Transport, provider.Endpoint, provider.AuthStrategy, provider.DefaultModel)
				if provider.Preset != "" {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  preset: %s\n", provider.Preset)
				}
				if len(provider.Command) > 0 {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  command: %s\n", strings.Join(provider.Command, " "))
				}
				if len(provider.Capabilities) > 0 {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  capabilities: %s\n", strings.Join(provider.Capabilities, ","))
				}
			}
			return nil
		},
	}
}

func newLLMUseCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Select the default LLM provider entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			cfg, err := config.LoadForEdit(*configPath)
			if err != nil {
				return err
			}
			for _, provider := range cfg.LLMs.Providers {
				if provider.ID == id {
					cfg.LLMs.Default = id
					if err := config.Write(*configPath, cfg); err != nil {
						return err
					}
					return refreshDaemonAfterMutation(cfg, "updated llm provider config")
				}
			}
			return fmt.Errorf("llm provider %q not found", id)
		},
	}
}

func newLLMLoginCommand(configPath *string) *cobra.Command {
	var input credentialInput
	cmd := &cobra.Command{
		Use:   "login <name>",
		Short: "Authenticate an LLM provider entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadForEdit(*configPath)
			if err != nil {
				return err
			}
			provider, err := findLLMProvider(cfg, args[0])
			if err != nil {
				return err
			}
			credential, err := credentialFromInput(provider.AuthStrategy, provider.Endpoint, input)
			if err != nil {
				return err
			}
			if err := secretstore.Save(provider.CredentialRef, credential); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "authenticated llm provider %s\n", provider.ID)
			return refreshDaemonAfterMutation(cfg, "updated llm provider auth")
		},
	}
	addCredentialFlags(cmd, &input)
	return cmd
}

func newLLMLogoutCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "logout <name>",
		Short: "Clear authentication for an LLM provider entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadForEdit(*configPath)
			if err != nil {
				return err
			}
			provider, err := findLLMProvider(cfg, args[0])
			if err != nil {
				return err
			}
			if err := secretstore.Delete(provider.CredentialRef); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "cleared llm provider auth for %s\n", provider.ID)
			return refreshDaemonAfterMutation(cfg, "updated llm provider auth")
		},
	}
}

func credentialRefForLLM(id string) string {
	return fmt.Sprintf("keyring://rillan/llm/%s", strings.TrimSpace(id))
}

func buildLLMProviderEntry(id string, preset string, backend string, transport string, endpoint string, command []string, authStrategy string, defaultModel string, capabilities []string) (config.LLMProviderConfig, error) {
	entry := config.LLMProviderConfig{
		ID:            strings.TrimSpace(id),
		Preset:        strings.ToLower(strings.TrimSpace(preset)),
		Backend:       normalizeLLMBackend(backend, transport),
		Transport:     normalizeLLMTransport(transport),
		Endpoint:      strings.TrimSpace(endpoint),
		Command:       normalizeCommand(command),
		AuthStrategy:  normalizeAuthStrategy(authStrategy, transport),
		DefaultModel:  strings.TrimSpace(defaultModel),
		Capabilities:  normalizeCapabilities(capabilities),
		CredentialRef: credentialRefForLLM(id),
	}
	if entry.Preset == "" {
		return entry, nil
	}
	presetConfig := config.BundledLLMProviderPreset(entry.Preset)
	if presetConfig.ID == "" {
		return config.LLMProviderConfig{}, fmt.Errorf("unsupported llm preset %q", entry.Preset)
	}
	presetEntry := presetConfig.ProviderConfig(entry.ID)
	presetEntry.Command = entry.Command
	if entry.Backend != "" {
		presetEntry.Backend = entry.Backend
	}
	if entry.Transport != "" {
		presetEntry.Transport = entry.Transport
	}
	if entry.Endpoint != "" {
		presetEntry.Endpoint = entry.Endpoint
	}
	if entry.AuthStrategy != "" {
		presetEntry.AuthStrategy = entry.AuthStrategy
	}
	if entry.DefaultModel != "" {
		presetEntry.DefaultModel = entry.DefaultModel
	}
	if len(entry.Capabilities) > 0 {
		presetEntry.Capabilities = entry.Capabilities
	}
	return presetEntry, nil
}

func normalizeLLMBackend(backend string, transport string) string {
	backend = strings.ToLower(strings.TrimSpace(backend))
	if backend != "" {
		return backend
	}
	if normalizeLLMTransport(transport) == config.LLMTransportHTTP {
		return config.LLMBackendOpenAICompatible
	}
	return ""
}

func normalizeLLMTransport(transport string) string {
	transport = strings.ToLower(strings.TrimSpace(transport))
	if transport == "" {
		return config.LLMTransportHTTP
	}
	return transport
}

func normalizeAuthStrategy(authStrategy string, transport string) string {
	authStrategy = strings.ToLower(strings.TrimSpace(authStrategy))
	if authStrategy != "" {
		return authStrategy
	}
	switch normalizeLLMTransport(transport) {
	case config.LLMTransportSTDIO:
		return config.AuthStrategyNone
	case config.LLMTransportHTTP:
		return config.AuthStrategyAPIKey
	default:
		return ""
	}
}

func normalizeCommand(command []string) []string {
	result := make([]string, 0, len(command))
	for _, item := range command {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func normalizeCapabilities(capabilities []string) []string {
	if len(capabilities) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(capabilities))
	result := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		normalized := strings.ToLower(strings.TrimSpace(capability))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	sort.Strings(result)
	return result
}

func validateLLMProviderEntry(entry config.LLMProviderConfig) error {
	if entry.ID == "" {
		return fmt.Errorf("llm provider name must not be empty")
	}
	if entry.Backend == "" {
		return fmt.Errorf("llm provider backend must not be empty")
	}
	switch entry.Transport {
	case config.LLMTransportHTTP:
		if entry.Endpoint == "" {
			return fmt.Errorf("llm provider endpoint must not be empty when transport is %q", config.LLMTransportHTTP)
		}
	case config.LLMTransportSTDIO:
		if len(entry.Command) == 0 {
			return fmt.Errorf("llm provider command must not be empty when transport is %q", config.LLMTransportSTDIO)
		}
	default:
		return fmt.Errorf("unsupported llm provider transport %q", entry.Transport)
	}
	switch entry.AuthStrategy {
	case config.AuthStrategyNone, config.AuthStrategyAPIKey, config.AuthStrategyBrowserOIDC, config.AuthStrategyDeviceOIDC:
	default:
		return fmt.Errorf("unsupported llm auth strategy %q", entry.AuthStrategy)
	}
	return nil
}

func findLLMProvider(cfg config.Config, id string) (config.LLMProviderConfig, error) {
	id = strings.TrimSpace(id)
	for _, provider := range cfg.LLMs.Providers {
		if provider.ID == id {
			return provider, nil
		}
	}
	return config.LLMProviderConfig{}, fmt.Errorf("llm provider %q not found", id)
}
