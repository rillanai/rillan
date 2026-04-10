// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"

	"github.com/rillanai/rillan/internal/config"
	"github.com/spf13/cobra"
)

func newInitCommand() *cobra.Command {
	var outputPath string
	var projectOutputPath string
	var force bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a starter config for Rillan",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.WriteExampleConfig(outputPath, force); err != nil {
				return err
			}
			if err := config.WriteExampleProjectConfig(projectOutputPath, force); err != nil {
				return err
			}

			_, err := fmt.Fprintf(cmd.OutOrStdout(), "wrote config to %s\nwrote project config to %s\n", outputPath, projectOutputPath)
			return err
		},
	}

	cmd.Flags().StringVar(&outputPath, "output", config.DefaultConfigPath(), "Path to write the starter config")
	cmd.Flags().StringVar(&projectOutputPath, "project-output", config.DefaultProjectConfigPath(""), "Path to write the starter project config")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite an existing file")

	return cmd
}
