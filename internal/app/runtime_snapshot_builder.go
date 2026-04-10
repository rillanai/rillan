// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rillanai/rillan/internal/classify"
	"github.com/rillanai/rillan/internal/config"
	"github.com/rillanai/rillan/internal/httpapi"
	"github.com/rillanai/rillan/internal/index"
	"github.com/rillanai/rillan/internal/modules"
	"github.com/rillanai/rillan/internal/ollama"
	"github.com/rillanai/rillan/internal/providers"
	"github.com/rillanai/rillan/internal/retrieval"
	"github.com/rillanai/rillan/internal/routing"
)

type runtimeSnapshotBuilder struct {
	configPath       string
	systemConfigPath string
	auditLedgerPath  string
}

const outboundHTTPTimeout = 30 * time.Second

func (b runtimeSnapshotBuilder) buildFromLoaded(ctx context.Context, cfg config.Config, project config.ProjectConfig, system *config.SystemConfig, projectConfigPath string) (*runtimeState, error) {
	snapshot, err := buildRuntimeSnapshot(ctx, cfg, project, system, b.auditLedgerPath, projectConfigPath)
	if err != nil {
		return nil, err
	}
	return &runtimeState{
		snapshot:          snapshot,
		projectConfigPath: projectConfigPath,
		systemConfigPath:  b.systemConfigPath,
	}, nil
}

func (b runtimeSnapshotBuilder) buildFromDisk(ctx context.Context) (*runtimeState, error) {
	cfg, err := config.Load(b.configPath)
	if err != nil {
		return nil, err
	}

	projectConfigPath := config.ResolveProjectConfigPath(cfg.Index.Root)
	projectCfg, err := config.LoadProject(projectConfigPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			projectCfg = config.DefaultProjectConfig()
		} else {
			return nil, err
		}
	}

	var systemCfg *config.SystemConfig
	loadedSystemCfg, err := config.LoadSystem(b.systemConfigPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	} else {
		systemCfg = &loadedSystemCfg
	}

	return b.buildFromLoaded(ctx, cfg, projectCfg, systemCfg, projectConfigPath)
}

func buildRuntimeSnapshot(ctx context.Context, cfg config.Config, project config.ProjectConfig, system *config.SystemConfig, auditLedgerPath string, projectConfigPath string) (httpapi.RuntimeSnapshot, error) {
	discoveredModules, err := modules.LoadProjectCatalog(projectConfigPath)
	if err != nil {
		return httpapi.RuntimeSnapshot{}, err
	}
	moduleCatalog, err := modules.FilterEnabled(discoveredModules, project.Modules.Enabled)
	if err != nil {
		return httpapi.RuntimeSnapshot{}, err
	}
	moduleCatalog, err = modules.FilterTrusted(moduleCatalog, projectConfigPath, system)
	if err != nil {
		return httpapi.RuntimeSnapshot{}, err
	}
	runtimeConfig, err := augmentRuntimeConfigWithModuleLLMAdapters(cfg, moduleCatalog)
	if err != nil {
		return httpapi.RuntimeSnapshot{}, err
	}

	providerHostCfg, err := config.ResolveRuntimeProviderHostConfig(runtimeConfig, project)
	if err != nil {
		return httpapi.RuntimeSnapshot{}, err
	}

	httpClient := &http.Client{Timeout: outboundHTTPTimeout}
	providerHost, err := providers.NewHost(providerHostCfg, httpClient)
	if err != nil {
		return httpapi.RuntimeSnapshot{}, err
	}

	provider, err := providerHost.DefaultProvider()
	if err != nil {
		return httpapi.RuntimeSnapshot{}, err
	}

	routeCatalog := routing.BuildCatalog(runtimeConfig, project)
	snapshot := httpapi.RuntimeSnapshot{
		Provider:      provider,
		ProviderHost:  providerHost,
		ProjectConfig: project,
		Config:        runtimeConfig,
		SystemConfig:  system,
		Modules:       moduleCatalog,
		RouteCatalog:  routeCatalog,
		RouteStatus: routing.BuildStatusCatalog(ctx, routing.StatusInput{
			Catalog:    routeCatalog,
			Config:     runtimeConfig,
			HTTPClient: httpClient,
		}),
		ReadinessInfo: httpapi.ReadinessInfo{
			RetrievalMode:      "disabled",
			SystemConfigLoaded: system != nil,
			AuditLedgerPath:    auditLedgerPath,
			LocalModelRequired: cfg.LocalModel.Enabled,
			ModulesDiscovered:  len(discoveredModules.Modules),
			ModulesEnabled:     len(moduleCatalog.Modules),
		},
	}
	if cfg.Retrieval.Enabled {
		snapshot.ReadinessInfo.RetrievalMode = "targeted_remote"
	}

	pipelineOpts := make([]retrieval.PipelineOption, 0, 2)
	if cfg.LocalModel.Enabled {
		ollamaClient := ollama.New(cfg.LocalModel.BaseURL, &http.Client{Timeout: outboundHTTPTimeout})
		snapshot.OllamaChecker = ollamaClient.Ping
		snapshot.Classifier = classify.NewSafeClassifier(classify.NewOllamaClassifier(ollamaClient, cfg.LocalModel.QueryRewrite.Model))

		pipelineOpts = append(pipelineOpts,
			retrieval.WithQueryEmbedder(
				retrieval.NewFallbackEmbedder(
					retrieval.NewOllamaEmbedder(ollamaClient, cfg.LocalModel.EmbedModel),
					retrieval.PlaceholderEmbedder{},
				),
			),
		)

		if cfg.LocalModel.QueryRewrite.Enabled {
			pipelineOpts = append(pipelineOpts,
				retrieval.WithQueryRewriter(retrieval.NewOllamaQueryRewriter(ollamaClient, cfg.LocalModel.QueryRewrite.Model)),
			)
		}
	}

	snapshot.Pipeline = retrieval.NewPipeline(cfg.Retrieval, index.DefaultDBPath(), pipelineOpts...)
	return snapshot, nil
}

func augmentRuntimeConfigWithModuleLLMAdapters(cfg config.Config, moduleCatalog modules.Catalog) (config.Config, error) {
	if cfg.SchemaVersion < config.SchemaVersionV2 || len(moduleCatalog.Modules) == 0 {
		return cfg, nil
	}

	augmented := cfg
	augmented.LLMs.Default = cfg.LLMs.Default
	augmented.LLMs.Providers = append([]config.LLMProviderConfig(nil), cfg.LLMs.Providers...)

	seenProviders := make(map[string]string, len(augmented.LLMs.Providers))
	for _, provider := range augmented.LLMs.Providers {
		providerID := strings.TrimSpace(provider.ID)
		if providerID == "" {
			continue
		}
		seenProviders[providerID] = "runtime config"
	}

	for _, module := range moduleCatalog.Modules {
		for _, adapter := range module.LLMAdapters {
			adapterID := strings.TrimSpace(adapter.ID)
			if adapterID == "" {
				continue
			}
			if source, exists := seenProviders[adapterID]; exists {
				return config.Config{}, errors.New("module llm adapter id collision: " + adapterID + " already declared in " + source)
			}
			seenProviders[adapterID] = "module " + module.ID
			augmented.LLMs.Providers = append(augmented.LLMs.Providers, adapter)
		}
	}

	return augmented, nil
}
