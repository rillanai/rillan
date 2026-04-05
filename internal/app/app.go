package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/rillanai/rillan/internal/audit"
	"github.com/rillanai/rillan/internal/config"
	"github.com/rillanai/rillan/internal/httpapi"
	"github.com/rillanai/rillan/internal/observability"
	"github.com/rillanai/rillan/internal/policy"
	"github.com/rillanai/rillan/internal/providers"
)

type App struct {
	addr       string
	configPath string
	logger     *slog.Logger
	runtime    *runtimeManager
	server     *http.Server
}

const (
	serverReadHeaderTimeout = 5 * time.Second
	serverReadTimeout       = 30 * time.Second
	serverWriteTimeout      = 60 * time.Second
	serverIdleTimeout       = 120 * time.Second
)

func New(cfg config.Config, project config.ProjectConfig, system *config.SystemConfig, configPath string, projectConfigPath string, systemConfigPath string, logger *slog.Logger) (*App, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Server.Auth.Enabled {
		if _, err := config.ResolveServerAuthBearer(cfg); err != nil {
			return nil, err
		}
	}
	auditStore, err := audit.NewStore(audit.DefaultLedgerPath())
	if err != nil {
		return nil, err
	}
	metrics := observability.NewRegistry()
	builder := runtimeSnapshotBuilder{
		configPath:       configPath,
		systemConfigPath: systemConfigPath,
		auditLedgerPath:  auditStore.Path(),
	}
	initialState, err := builder.buildFromLoaded(context.Background(), cfg, project, system, projectConfigPath)
	if err != nil {
		return nil, err
	}
	runtime := newRuntimeManager(initialState, builder.buildFromDisk, logger)
	providerHost, _ := initialState.snapshot.ProviderHost.(*providers.Host)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	server := &http.Server{
		Addr:              addr,
		ReadHeaderTimeout: serverReadHeaderTimeout,
		ReadTimeout:       serverReadTimeout,
		WriteTimeout:      serverWriteTimeout,
		IdleTimeout:       serverIdleTimeout,
		Handler: httpapi.NewRouter(logger, initialState.snapshot.Provider, cfg, httpapi.RouterOptions{
			ProjectConfig:      initialState.snapshot.ProjectConfig,
			SystemConfig:       initialState.snapshot.SystemConfig,
			SystemConfigLoaded: initialState.snapshot.ReadinessInfo.SystemConfigLoaded,
			AuditLedgerPath:    auditStore.Path(),
			AuditRecorder:      auditStore,
			PolicyEvaluator:    policy.NewEvaluator(),
			PolicyScanner:      policy.DefaultScanner(),
			Classifier:         initialState.snapshot.Classifier,
			ProviderHost:       providerHost,
			RouteCatalog:       initialState.snapshot.RouteCatalog,
			RouteStatus:        initialState.snapshot.RouteStatus,
			RetrievalMode:      initialState.snapshot.ReadinessInfo.RetrievalMode,
			LocalModelRequired: initialState.snapshot.ReadinessInfo.LocalModelRequired,
			OllamaChecker:      initialState.snapshot.OllamaChecker,
			RuntimeSnapshot:    runtime.CurrentSnapshot,
			RefreshRuntime:     runtime.Refresh,
			Metrics:            metrics,
		}),
	}

	return &App{
		addr:       addr,
		configPath: configPath,
		logger:     logger,
		runtime:    runtime,
		server:     server,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	state := a.runtime.CurrentState()
	a.logger.Info("starting rillan server",
		"addr", a.addr,
		"config_path", a.configPath,
		"project_config_path", state.projectConfigPath,
		"system_config_path", state.systemConfigPath,
		"system_config_loaded", state.snapshot.ReadinessInfo.SystemConfigLoaded,
		"provider", state.snapshot.Provider.Name(),
	)

	go func() {
		<-ctx.Done()
		a.logger.Info("server shutdown started", "addr", a.addr)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5e9)
		defer cancel()
		if err := a.server.Shutdown(shutdownCtx); err != nil {
			a.logger.Error("server shutdown failed", "error", err.Error())
			return
		}
		a.logger.Info("server shutdown completed", "addr", a.addr)
	}()

	err := a.server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}

	return err
}
