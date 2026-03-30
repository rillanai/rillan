package main

import (
	"fmt"
	"strings"

	"github.com/sidekickos/rillan/internal/config"
	"github.com/sidekickos/rillan/internal/secretstore"
	"github.com/spf13/cobra"
)

func newAuthCommand() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage Rillan team and control-plane authentication",
	}

	cmd.PersistentFlags().StringVar(&configPath, "config", config.DefaultConfigPath(), "Path to the runtime config file")
	cmd.AddCommand(newAuthLoginCommand(&configPath))
	cmd.AddCommand(newAuthLogoutCommand(&configPath))
	cmd.AddCommand(newAuthStatusCommand(&configPath))

	return cmd
}

func newAuthLoginCommand(configPath *string) *cobra.Command {
	var endpoint string
	var authStrategy string
	var input credentialInput

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log into a Rillan team endpoint",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadForEdit(*configPath)
			if err != nil {
				return err
			}
			if strings.TrimSpace(endpoint) == "" {
				endpoint = cfg.Auth.Rillan.Endpoint
			}
			if strings.TrimSpace(authStrategy) == "" {
				authStrategy = cfg.Auth.Rillan.AuthStrategy
			}
			if strings.TrimSpace(endpoint) == "" {
				return fmt.Errorf("--endpoint is required when no control-plane endpoint is configured")
			}
			if strings.TrimSpace(authStrategy) == "" {
				return fmt.Errorf("--auth-strategy is required when no control-plane auth strategy is configured")
			}
			credential, err := credentialFromInput(authStrategy, strings.TrimSpace(endpoint), input)
			if err != nil {
				return err
			}
			cfg.Auth.Rillan.Endpoint = strings.TrimSpace(endpoint)
			cfg.Auth.Rillan.AuthStrategy = strings.TrimSpace(strings.ToLower(authStrategy))
			if cfg.Auth.Rillan.SessionRef == "" {
				cfg.Auth.Rillan.SessionRef = "keyring://rillan/auth/control-plane"
			}
			if err := secretstore.Save(cfg.Auth.Rillan.SessionRef, credential); err != nil {
				return err
			}
			if err := config.Write(*configPath, cfg); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "logged into control plane at %s\n", cfg.Auth.Rillan.Endpoint)
			return refreshDaemonAfterMutation(cfg, "updated control-plane auth")
		},
	}
	cmd.Flags().StringVar(&endpoint, "endpoint", "", "Rillan control-plane endpoint URL")
	cmd.Flags().StringVar(&authStrategy, "auth-strategy", "", "Auth strategy (api_key, browser_oidc, device_oidc)")
	addCredentialFlags(cmd, &input)
	return cmd
}

func newAuthLogoutCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Log out of the active Rillan team endpoint",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadForEdit(*configPath)
			if err != nil {
				return err
			}
			if strings.TrimSpace(cfg.Auth.Rillan.SessionRef) == "" {
				return nil
			}
			if err := secretstore.Delete(cfg.Auth.Rillan.SessionRef); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "logged out of control plane\n")
			return refreshDaemonAfterMutation(cfg, "updated control-plane auth")
		},
	}
}

func newAuthStatusCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show Rillan team authentication state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadForEdit(*configPath)
			if err != nil {
				return err
			}
			sessionPresent := false
			if strings.TrimSpace(cfg.Auth.Rillan.SessionRef) != "" {
				sessionPresent = secretstore.Exists(cfg.Auth.Rillan.SessionRef)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "endpoint: %s\nauth_strategy: %s\nsession_ref: %s\nlogged_in: %t\n", cfg.Auth.Rillan.Endpoint, cfg.Auth.Rillan.AuthStrategy, cfg.Auth.Rillan.SessionRef, sessionPresent)
			return nil
		},
	}
}
