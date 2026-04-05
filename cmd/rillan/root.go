package main

import (
	"context"

	"github.com/rillanai/rillan/internal/version"
	"github.com/spf13/cobra"
)

func newRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "rillan",
		Short:         "Local OpenAI-compatible proxy daemon",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version.String(),
	}

	cmd.AddCommand(newServeCommand())
	cmd.AddCommand(newInitCommand())
	cmd.AddCommand(newIndexCommand())
	cmd.AddCommand(newStatusCommand())
	cmd.AddCommand(newAuthCommand())
	cmd.AddCommand(newLLMCommand())
	cmd.AddCommand(newMCPCommand())
	cmd.AddCommand(newSkillCommand())
	cmd.AddCommand(newConfigCommand())

	return cmd
}

func execute(ctx context.Context) error {
	return newRootCommand().ExecuteContext(ctx)
}
