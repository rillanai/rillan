package main

import (
	"log/slog"
	"os"

	"github.com/sidekickos/rillan/internal/app"
	"github.com/sidekickos/rillan/internal/config"
	"github.com/spf13/cobra"
)

func newServeCommand() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the local Rillan API daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}

			logger := newLogger(cfg.Server.LogLevel)
			application, err := app.New(cfg, configPath, logger)
			if err != nil {
				return err
			}

			return application.Run(cmd.Context())
		},
	}

	cmd.Flags().StringVar(&configPath, "config", config.DefaultConfigPath(), "Path to the runtime config file")

	return cmd
}

func newLogger(level string) *slog.Logger {
	logLevel := new(slog.LevelVar)
	logLevel.Set(config.ParseLogLevel(level))

	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
}
