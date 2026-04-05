package main

import (
	"errors"
	"log/slog"
	"os"

	"github.com/rillanai/rillan/internal/app"
	"github.com/rillanai/rillan/internal/config"
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

			projectConfigPath := config.ResolveProjectConfigPath(cfg.Index.Root)
			projectCfg, err := config.LoadProject(projectConfigPath)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					projectCfg = config.DefaultProjectConfig()
				} else {
					return err
				}
			}

			systemConfigPath := config.ResolveSystemConfigPath()
			var systemCfg *config.SystemConfig
			loadedSystemCfg, err := config.LoadSystem(systemConfigPath)
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					return err
				}
			} else {
				systemCfg = &loadedSystemCfg
			}

			logger := newLogger(cfg.Server.LogLevel)
			application, err := app.New(cfg, projectCfg, systemCfg, configPath, projectConfigPath, systemConfigPath, logger)
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
