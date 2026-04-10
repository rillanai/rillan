// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package httpapi

import (
	"context"

	"github.com/rillanai/rillan/internal/classify"
	"github.com/rillanai/rillan/internal/config"
	"github.com/rillanai/rillan/internal/modules"
	"github.com/rillanai/rillan/internal/providers"
	"github.com/rillanai/rillan/internal/retrieval"
	"github.com/rillanai/rillan/internal/routing"
)

type RuntimeSnapshot struct {
	Provider      providers.Provider
	ProviderHost  providerHost
	Pipeline      *retrieval.Pipeline
	Config        config.Config
	ProjectConfig config.ProjectConfig
	SystemConfig  *config.SystemConfig
	Modules       modules.Catalog
	Classifier    classify.Classifier
	RouteCatalog  routing.Catalog
	RouteStatus   routing.StatusCatalog
	ReadinessInfo ReadinessInfo
	OllamaChecker func(context.Context) error
}

type RuntimeSnapshotFunc func() RuntimeSnapshot
