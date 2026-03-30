package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/sidekickos/rillan/internal/audit"
	"github.com/sidekickos/rillan/internal/classify"
	"github.com/sidekickos/rillan/internal/config"
	"github.com/sidekickos/rillan/internal/httpapi"
	"github.com/sidekickos/rillan/internal/ollama"
	"github.com/sidekickos/rillan/internal/policy"
	"github.com/sidekickos/rillan/internal/providers"
	"github.com/sidekickos/rillan/internal/retrieval"
	"github.com/sidekickos/rillan/internal/routing"
)

type App struct {
	addr               string
	configPath         string
	projectConfigPath  string
	systemConfigPath   string
	systemConfigLoaded bool
	logger             *slog.Logger
	provider           providers.Provider
	server             *http.Server
}

func New(cfg config.Config, project config.ProjectConfig, system *config.SystemConfig, configPath string, projectConfigPath string, systemConfigPath string, logger *slog.Logger) (*App, error) {
	if logger == nil {
		logger = slog.Default()
	}

	providerHostCfg, err := config.ResolveRuntimeProviderHostConfig(cfg, project)
	if err != nil {
		return nil, err
	}

	httpClient := &http.Client{}
	providerHost, err := providers.NewHost(providerHostCfg, httpClient)
	if err != nil {
		return nil, err
	}

	provider, err := providerHost.DefaultProvider()
	if err != nil {
		return nil, err
	}

	var routerOpts httpapi.RouterOptions
	routerOpts.ProjectConfig = project
	routerOpts.SystemConfig = system
	routerOpts.SystemConfigLoaded = system != nil
	auditStore, err := audit.NewStore(audit.DefaultLedgerPath())
	if err != nil {
		return nil, err
	}
	routerOpts.AuditLedgerPath = auditStore.Path()
	routerOpts.AuditRecorder = auditStore
	routerOpts.PolicyEvaluator = policy.NewEvaluator()
	routerOpts.PolicyScanner = policy.DefaultScanner()
	routerOpts.ProviderHost = providerHost
	routerOpts.RouteCatalog = routing.BuildCatalog(cfg, project)
	routerOpts.RouteStatus = routing.BuildStatusCatalog(context.Background(), routing.StatusInput{
		Catalog:    routerOpts.RouteCatalog,
		Config:     cfg,
		HTTPClient: httpClient,
	})
	if cfg.Retrieval.Enabled {
		routerOpts.RetrievalMode = "targeted_remote"
	} else {
		routerOpts.RetrievalMode = "disabled"
	}
	routerOpts.LocalModelRequired = cfg.LocalModel.Enabled

	if cfg.LocalModel.Enabled {
		ollamaClient := ollama.New(cfg.LocalModel.BaseURL, &http.Client{})
		routerOpts.OllamaChecker = ollamaClient.Ping
		routerOpts.Classifier = classify.NewSafeClassifier(classify.NewOllamaClassifier(ollamaClient, cfg.LocalModel.QueryRewrite.Model))

		routerOpts.PipelineOpts = append(routerOpts.PipelineOpts,
			retrieval.WithQueryEmbedder(
				retrieval.NewFallbackEmbedder(
					retrieval.NewOllamaEmbedder(ollamaClient, cfg.LocalModel.EmbedModel),
					retrieval.PlaceholderEmbedder{},
				),
			),
		)

		if cfg.LocalModel.QueryRewrite.Enabled {
			routerOpts.PipelineOpts = append(routerOpts.PipelineOpts,
				retrieval.WithQueryRewriter(retrieval.NewOllamaQueryRewriter(ollamaClient, cfg.LocalModel.QueryRewrite.Model)),
			)
		}
	}

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	server := &http.Server{
		Addr:    addr,
		Handler: httpapi.NewRouter(logger, provider, cfg, routerOpts),
	}

	return &App{
		addr:               addr,
		configPath:         configPath,
		projectConfigPath:  projectConfigPath,
		systemConfigPath:   systemConfigPath,
		systemConfigLoaded: system != nil,
		logger:             logger,
		provider:           provider,
		server:             server,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	a.logger.Info("starting rillan server",
		"addr", a.addr,
		"config_path", a.configPath,
		"project_config_path", a.projectConfigPath,
		"system_config_path", a.systemConfigPath,
		"system_config_loaded", a.systemConfigLoaded,
		"provider", a.provider.Name(),
	)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5e9)
		defer cancel()
		if err := a.server.Shutdown(shutdownCtx); err != nil {
			a.logger.Error("server shutdown failed", "error", err.Error())
		}
	}()

	err := a.server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}

	return err
}
