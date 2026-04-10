// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/rillanai/rillan/internal/httpapi"
)

type runtimeState struct {
	snapshot          httpapi.RuntimeSnapshot
	projectConfigPath string
	systemConfigPath  string
}

type runtimeBuilder func(context.Context) (*runtimeState, error)

type runtimeManager struct {
	current atomic.Pointer[runtimeState]
	build   runtimeBuilder
	logger  *slog.Logger
	mu      sync.Mutex
}

func newRuntimeManager(initial *runtimeState, build runtimeBuilder, logger *slog.Logger) *runtimeManager {
	if logger == nil {
		logger = slog.Default()
	}

	manager := &runtimeManager{build: build, logger: logger}
	manager.current.Store(initial)
	return manager
}

func (m *runtimeManager) CurrentSnapshot() httpapi.RuntimeSnapshot {
	state := m.current.Load()
	if state == nil {
		return httpapi.RuntimeSnapshot{}
	}
	return state.snapshot
}

func (m *runtimeManager) CurrentState() *runtimeState {
	return m.current.Load()
}

func (m *runtimeManager) Refresh(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	next, err := m.build(ctx)
	if err != nil {
		return err
	}
	previous := m.current.Load()
	m.current.Store(next)
	m.logger.Info("runtime snapshot refreshed",
		"project_config_path", next.projectConfigPath,
		"system_config_path", next.systemConfigPath,
		"system_config_loaded", next.snapshot.ReadinessInfo.SystemConfigLoaded,
		"provider", next.snapshot.Provider.Name(),
	)
	if previous != nil {
		previousReady := previous.snapshot.RouteStatus.ByID[previous.snapshot.Provider.Name()].Available
		nextReady := next.snapshot.RouteStatus.ByID[next.snapshot.Provider.Name()].Available
		if previousReady != nextReady {
			m.logger.Info("runtime readiness changed", "provider", next.snapshot.Provider.Name(), "ready", nextReady)
		}
	}
	return nil
}
