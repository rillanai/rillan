package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/sidekickos/rillan/internal/config"
	"github.com/sidekickos/rillan/internal/httpapi"
	"github.com/sidekickos/rillan/internal/providers"
)

type App struct {
	addr       string
	configPath string
	logger     *slog.Logger
	provider   providers.Provider
	server     *http.Server
}

func New(cfg config.Config, configPath string, logger *slog.Logger) (*App, error) {
	if logger == nil {
		logger = slog.Default()
	}

	provider, err := providers.New(cfg.Provider, &http.Client{})
	if err != nil {
		return nil, err
	}

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	server := &http.Server{
		Addr:    addr,
		Handler: httpapi.NewRouter(logger, provider),
	}

	return &App{
		addr:       addr,
		configPath: configPath,
		logger:     logger,
		provider:   provider,
		server:     server,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	a.logger.Info("starting rillan server",
		"addr", a.addr,
		"config_path", a.configPath,
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
