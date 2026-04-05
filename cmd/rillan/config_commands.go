package main

import (
	"github.com/rillanai/rillan/internal/config"
	"github.com/spf13/cobra"
)

func newConfigCommand() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect and mutate Rillan configuration",
	}
	cmd.PersistentFlags().StringVar(&configPath, "config", config.DefaultConfigPath(), "Path to the runtime config file")

	cmd.AddCommand(newPlaceholderLeafCommand("get", "Read a configuration value"))
	cmd.AddCommand(newPlaceholderLeafCommand("set", "Write a configuration value"))
	cmd.AddCommand(newPlaceholderLeafCommand("list", "List configuration values"))

	return cmd
}
