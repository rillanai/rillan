package main

import (
	"context"

	"github.com/sidekickos/rillan/internal/version"
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

	return cmd
}

func execute(ctx context.Context) error {
	return newRootCommand().ExecuteContext(ctx)
}
