package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/rillanai/rillan/internal/config"
	"github.com/rillanai/rillan/internal/index"
	"github.com/rillanai/rillan/internal/ollama"
	"github.com/spf13/cobra"
)

func newIndexCommand() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "index",
		Short: "Build or rebuild the local index",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadWithMode(configPath, config.ValidationModeIndex)
			if err != nil {
				return err
			}

			logger := newLogger(cfg.Server.LogLevel)
			var opts []index.RebuildOption

			if cfg.LocalModel.Enabled {
				client := ollama.New(cfg.LocalModel.BaseURL, &http.Client{})
				reachable := client.Ping(cmd.Context()) == nil
				if !reachable {
					return fmt.Errorf("ollama unavailable at %s while local_model.enabled is true", cfg.LocalModel.BaseURL)
				}
				opts = append(opts, index.WithEmbedder(&ollamaEmbedderAdapter{client: client}, reachable))
			}

			status, err := index.Rebuild(cmd.Context(), cfg, logger, opts...)
			if err != nil {
				return err
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "index complete\nroot: %s\ndocuments: %d\nchunks: %d\nvectors: %d\ndb_path: %s\n", status.CommittedRootPath, status.Documents, status.Chunks, status.Vectors, status.DBPath)
			return err
		},
	}

	cmd.Flags().StringVar(&configPath, "config", config.DefaultConfigPath(), "Path to the runtime config file")

	return cmd
}

// ollamaEmbedderAdapter adapts ollama.Client to the index.Embedder interface.
type ollamaEmbedderAdapter struct {
	client *ollama.Client
}

func (a *ollamaEmbedderAdapter) Embed(ctx context.Context, model string, text string) ([]float32, error) {
	return a.client.Embed(ctx, model, text)
}
