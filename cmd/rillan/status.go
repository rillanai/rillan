package main

import (
	"fmt"

	"github.com/sidekickos/rillan/internal/config"
	"github.com/sidekickos/rillan/internal/index"
	"github.com/spf13/cobra"
)

func newStatusCommand() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show local index status",
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := config.LoadWithMode(configPath, config.ValidationModeStatus); err != nil {
				return err
			}

			status, err := index.ReadStatus(cmd.Context())
			if err != nil {
				return err
			}

			lastIndexed := "never"
			if !status.LastIndexedAt.IsZero() {
				lastIndexed = status.LastIndexedAt.Format("2006-01-02T15:04:05Z07:00")
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "state: %s\nroot: %s\ndocuments: %d\nchunks: %d\nvectors: %d\nlast_indexed_at: %s\ndb_path: %s\n", status.State, status.RootPath, status.Documents, status.Chunks, status.Vectors, lastIndexed, status.DBPath)
			return err
		},
	}

	cmd.Flags().StringVar(&configPath, "config", config.DefaultConfigPath(), "Path to the runtime config file")

	return cmd
}
