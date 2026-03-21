package main

import (
	"fmt"

	"github.com/sidekickos/rillan/internal/config"
	"github.com/spf13/cobra"
)

func newInitCommand() *cobra.Command {
	var outputPath string
	var force bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a starter config for Rillan",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.WriteExampleConfig(outputPath, force); err != nil {
				return err
			}

			_, err := fmt.Fprintf(cmd.OutOrStdout(), "wrote config to %s\n", outputPath)
			return err
		},
	}

	cmd.Flags().StringVar(&outputPath, "output", config.DefaultConfigPath(), "Path to write the starter config")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite an existing file")

	return cmd
}
