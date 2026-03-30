package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sidekickos/rillan/internal/config"
	"github.com/sidekickos/rillan/internal/secretstore"
	"github.com/spf13/cobra"
)

func newMCPCommand() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Manage named MCP endpoints and endpoint-bound auth",
	}

	cmd.PersistentFlags().StringVar(&configPath, "config", config.DefaultConfigPath(), "Path to the runtime config file")

	cmd.AddCommand(newMCPAddCommand(&configPath))
	cmd.AddCommand(newMCPRemoveCommand(&configPath))
	cmd.AddCommand(newMCPListCommand(&configPath))
	cmd.AddCommand(newMCPUseCommand(&configPath))
	cmd.AddCommand(newMCPLoginCommand(&configPath))
	cmd.AddCommand(newMCPLogoutCommand(&configPath))

	return cmd
}

func newMCPAddCommand(configPath *string) *cobra.Command {
	var endpoint string
	var transport string
	var command []string
	var authStrategy string
	var readOnly bool

	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a named MCP endpoint entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			entry := config.MCPServerConfig{
				ID:            strings.TrimSpace(args[0]),
				Endpoint:      strings.TrimSpace(endpoint),
				Transport:     normalizeMCPTransport(transport),
				Command:       normalizeCommand(command),
				AuthStrategy:  normalizeMCPAuthStrategy(authStrategy),
				ReadOnly:      readOnly,
				CredentialRef: credentialRefForMCP(args[0]),
			}
			if err := validateMCPServerEntry(entry); err != nil {
				return err
			}

			cfg, err := config.LoadForEdit(*configPath)
			if err != nil {
				return err
			}
			for _, existing := range cfg.MCPs.Servers {
				if existing.ID == entry.ID {
					return fmt.Errorf("mcp server %q already exists", entry.ID)
				}
			}
			cfg.MCPs.Servers = append(cfg.MCPs.Servers, entry)
			if cfg.MCPs.Default == "" {
				cfg.MCPs.Default = entry.ID
			}
			return config.Write(*configPath, cfg)
		},
	}

	cmd.Flags().StringVar(&endpoint, "endpoint", "", "MCP endpoint URL")
	cmd.Flags().StringVar(&transport, "transport", "http", "MCP transport type")
	cmd.Flags().StringSliceVar(&command, "command", nil, "MCP command for stdio transport")
	cmd.Flags().StringVar(&authStrategy, "auth-strategy", "none", "Auth strategy (none, api_key, browser_oidc, device_oidc)")
	cmd.Flags().BoolVar(&readOnly, "read-only", true, "Whether this MCP endpoint is read-only")

	return cmd
}

func newMCPRemoveCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a named MCP endpoint entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			cfg, err := config.LoadForEdit(*configPath)
			if err != nil {
				return err
			}

			servers := make([]config.MCPServerConfig, 0, len(cfg.MCPs.Servers))
			removed := false
			for _, server := range cfg.MCPs.Servers {
				if server.ID == id {
					removed = true
					continue
				}
				servers = append(servers, server)
			}
			if !removed {
				return fmt.Errorf("mcp server %q not found", id)
			}
			cfg.MCPs.Servers = servers
			if cfg.MCPs.Default == id {
				cfg.MCPs.Default = ""
			}
			return config.Write(*configPath, cfg)
		},
	}
}

func newMCPListCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured MCP endpoint entries",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadForEdit(*configPath)
			if err != nil {
				return err
			}
			servers := append([]config.MCPServerConfig(nil), cfg.MCPs.Servers...)
			sort.Slice(servers, func(i, j int) bool { return servers[i].ID < servers[j].ID })

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "default: %s\n", cfg.MCPs.Default)
			for _, server := range servers {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "- id: %s\n  endpoint: %s\n  transport: %s\n  auth_strategy: %s\n  read_only: %t\n", server.ID, server.Endpoint, server.Transport, server.AuthStrategy, server.ReadOnly)
				if len(server.Command) > 0 {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  command: %s\n", strings.Join(server.Command, " "))
				}
			}
			return nil
		},
	}
}

func newMCPUseCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Select the default MCP endpoint entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			cfg, err := config.LoadForEdit(*configPath)
			if err != nil {
				return err
			}
			for _, server := range cfg.MCPs.Servers {
				if server.ID == id {
					cfg.MCPs.Default = id
					return config.Write(*configPath, cfg)
				}
			}
			return fmt.Errorf("mcp server %q not found", id)
		},
	}
}

func newMCPLoginCommand(configPath *string) *cobra.Command {
	var input credentialInput
	cmd := &cobra.Command{
		Use:   "login <name>",
		Short: "Authenticate an MCP endpoint entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadForEdit(*configPath)
			if err != nil {
				return err
			}
			server, err := findMCPServer(cfg, args[0])
			if err != nil {
				return err
			}
			credential, err := credentialFromInput(server.AuthStrategy, server.Endpoint, input)
			if err != nil {
				return err
			}
			if err := secretstore.Save(server.CredentialRef, credential); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "authenticated mcp endpoint %s\n", server.ID)
			return nil
		},
	}
	addCredentialFlags(cmd, &input)
	return cmd
}

func newMCPLogoutCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "logout <name>",
		Short: "Clear authentication for an MCP endpoint entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadForEdit(*configPath)
			if err != nil {
				return err
			}
			server, err := findMCPServer(cfg, args[0])
			if err != nil {
				return err
			}
			if err := secretstore.Delete(server.CredentialRef); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "cleared mcp auth for %s\n", server.ID)
			return nil
		},
	}
}

func credentialRefForMCP(id string) string {
	return fmt.Sprintf("keyring://rillan/mcp/%s", strings.TrimSpace(id))
}

func normalizeMCPTransport(transport string) string {
	transport = strings.ToLower(strings.TrimSpace(transport))
	if transport == "" {
		return "http"
	}
	return transport
}

func normalizeMCPAuthStrategy(authStrategy string) string {
	authStrategy = strings.ToLower(strings.TrimSpace(authStrategy))
	if authStrategy == "" {
		return config.AuthStrategyNone
	}
	return authStrategy
}

func validateMCPServerEntry(entry config.MCPServerConfig) error {
	if entry.ID == "" {
		return fmt.Errorf("mcp server name must not be empty")
	}
	switch entry.Transport {
	case "http":
		if entry.Endpoint == "" {
			return fmt.Errorf("mcp server endpoint must not be empty when transport is %q", entry.Transport)
		}
	case "stdio":
		if len(entry.Command) == 0 {
			return fmt.Errorf("mcp server command must not be empty when transport is %q", entry.Transport)
		}
	default:
		return fmt.Errorf("unsupported mcp transport %q", entry.Transport)
	}
	switch entry.AuthStrategy {
	case config.AuthStrategyNone, config.AuthStrategyAPIKey, config.AuthStrategyBrowserOIDC, config.AuthStrategyDeviceOIDC:
	default:
		return fmt.Errorf("unsupported mcp auth strategy %q", entry.AuthStrategy)
	}
	return nil
}

func findMCPServer(cfg config.Config, id string) (config.MCPServerConfig, error) {
	id = strings.TrimSpace(id)
	for _, server := range cfg.MCPs.Servers {
		if server.ID == id {
			return server, nil
		}
	}
	return config.MCPServerConfig{}, fmt.Errorf("mcp server %q not found", id)
}
