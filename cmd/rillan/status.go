package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rillanai/rillan/internal/audit"
	"github.com/rillanai/rillan/internal/config"
	"github.com/rillanai/rillan/internal/index"
	"github.com/rillanai/rillan/internal/modules"
	"github.com/rillanai/rillan/internal/ollama"
	"github.com/spf13/cobra"
)

func newStatusCommand() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show local index status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadWithMode(configPath, config.ValidationModeStatus)
			if err != nil {
				return err
			}

			projectConfigPath := config.ResolveProjectConfigPath(cfg.Index.Root)
			projectCfg, err := loadStatusProjectConfig(projectConfigPath)
			if err != nil {
				return err
			}
			discoveredModules, err := modules.LoadProjectCatalog(projectConfigPath)
			if err != nil {
				return err
			}
			enabledModules, err := modules.FilterEnabled(discoveredModules, projectCfg.Modules.Enabled)
			if err != nil {
				return err
			}

			var systemCfg *config.SystemConfig

			systemConfigPath := config.ResolveSystemConfigPath()
			systemConfigState := "missing"
			if loadedSystemCfg, err := config.LoadSystem(systemConfigPath); err == nil {
				systemConfigState = "loaded"
				systemCfg = &loadedSystemCfg
			} else if !errors.Is(err, os.ErrNotExist) {
				systemConfigState = "invalid"
			}
			enabledModules, err = modules.FilterTrusted(enabledModules, projectConfigPath, systemCfg)
			if err != nil {
				return err
			}

			status, err := index.ReadStatus(cmd.Context(), cfg)
			if err != nil {
				return err
			}

			retrievalMode := "disabled"
			if cfg.Retrieval.Enabled {
				retrievalMode = "targeted_remote"
			}
			auditLedgerPath := audit.DefaultLedgerPath()

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "configured_root: %s\nlast_attempt_state: %s\nlast_attempt_root: %s\nlast_attempt_at: %s\nlast_attempt_error: %s\ncommitted_root: %s\ncommitted_last_indexed_at: %s\ndocuments: %d\nchunks: %d\nvectors: %d\ndb_path: %s\nsystem_config_path: %s\nsystem_config_state: %s\naudit_ledger_path: %s\nretrieval_enabled: %t\nretrieval_mode: %s\nmodules_discovered: %d\nmodules_enabled: %d\nmodule_ids: %s\n",
				emptyFallback(status.ConfiguredRootPath, "not configured"),
				emptyFallback(status.LastAttemptState, index.RunStatusNeverIndexed),
				emptyFallback(status.LastAttemptRootPath, "none"),
				formatStatusTime(status.LastAttemptAt),
				emptyFallback(status.LastAttemptError, "none"),
				emptyFallback(status.CommittedRootPath, "none"),
				formatStatusTime(status.CommittedIndexedAt),
				status.Documents,
				status.Chunks,
				status.Vectors,
				status.DBPath,
				systemConfigPath,
				systemConfigState,
				auditLedgerPath,
				cfg.Retrieval.Enabled,
				retrievalMode,
				len(discoveredModules.Modules),
				len(enabledModules.Modules),
				moduleIDs(enabledModules),
			)
			if err != nil {
				return err
			}

			// Local model status
			if cfg.LocalModel.Enabled {
				client := ollama.New(cfg.LocalModel.BaseURL, nil)
				pingCtx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
				reachable := client.Ping(pingCtx) == nil
				cancel()
				runtimeState := "ready"
				if !reachable {
					runtimeState = "degraded"
				}
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "local_model_enabled: true\nlocal_model_required: true\nlocal_model_url: %s\nlocal_model_reachable: %t\nlocal_model_embed_model: %s\nlocal_model_query_rewrite: %t\nruntime_state: %s\n",
					cfg.LocalModel.BaseURL,
					reachable,
					cfg.LocalModel.EmbedModel,
					cfg.LocalModel.QueryRewrite.Enabled,
					runtimeState,
				)
			} else {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "local_model_enabled: false\nlocal_model_required: false\nruntime_state: ready\n")
			}
			return err
		},
	}

	cmd.Flags().StringVar(&configPath, "config", config.DefaultConfigPath(), "Path to the runtime config file")

	return cmd
}

func loadStatusProjectConfig(projectConfigPath string) (config.ProjectConfig, error) {
	projectCfg, err := config.LoadProject(projectConfigPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return config.DefaultProjectConfig(), nil
		}
		return config.ProjectConfig{}, err
	}
	return projectCfg, nil
}

func formatStatusTime(value time.Time) string {
	if value.IsZero() {
		return "never"
	}
	return value.Format("2006-01-02T15:04:05Z07:00")
}

func emptyFallback(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func moduleIDs(catalog modules.Catalog) string {
	if len(catalog.Modules) == 0 {
		return "none"
	}
	ids := make([]string, 0, len(catalog.Modules))
	for _, module := range catalog.Modules {
		ids = append(ids, module.ID)
	}
	return strings.Join(ids, ",")
}
