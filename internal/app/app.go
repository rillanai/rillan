package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/sidekickos/rillan/internal/classify"
	"github.com/sidekickos/rillan/internal/config"
	"github.com/sidekickos/rillan/internal/httpapi"
	"github.com/sidekickos/rillan/internal/ollama"
	"github.com/sidekickos/rillan/internal/policy"
	"github.com/sidekickos/rillan/internal/providers"
	"github.com/sidekickos/rillan/internal/retrieval"
)

type App struct {
	addr              string
	configPath        string
	projectConfigPath string
	logger            *slog.Logger
	provider          providers.Provider
	server            *http.Server
}

func New(cfg config.Config, project config.ProjectConfig, configPath string, projectConfigPath string, logger *slog.Logger) (*App, error) {
	if logger == nil {
		logger = slog.Default()
	}

	provider, err := providers.New(cfg.Provider, &http.Client{})
	if err != nil {
		return nil, err
	}

	var routerOpts httpapi.RouterOptions
	routerOpts.ProjectConfig = project
	routerOpts.PolicyEvaluator = policy.NewEvaluator()
	routerOpts.PolicyScanner = policy.DefaultScanner()

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
		addr:              addr,
		configPath:        configPath,
		projectConfigPath: projectConfigPath,
		logger:            logger,
		provider:          provider,
		server:            server,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	a.logger.Info("starting rillan server",
		"addr", a.addr,
		"config_path", a.configPath,
		"project_config_path", a.projectConfigPath,
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
