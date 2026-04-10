// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	internalopenai "github.com/rillanai/rillan/internal/openai"
)

type readinessChecker interface {
	Ready(context.Context) error
}

type ReadinessInfo struct {
	RetrievalMode      string
	SystemConfigLoaded bool
	AuditLedgerPath    string
	LocalModelRequired bool
	ModulesDiscovered  int
	ModulesEnabled     int
}

const readinessCheckTimeout = 3 * time.Second

func HealthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func ReadyHandler(checker readinessChecker, ollamaChecker func(context.Context) error, info ReadinessInfo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		serveReadyResponse(w, r, checker, ollamaChecker, info)
	}
}

func ReadyHandlerFromRuntime(current RuntimeSnapshotFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		snapshot := current()
		serveReadyResponse(w, r, snapshot.Provider, snapshot.OllamaChecker, snapshot.ReadinessInfo)
	}
}

func serveReadyResponse(w http.ResponseWriter, r *http.Request, checker readinessChecker, ollamaChecker func(context.Context) error, info ReadinessInfo) {
	if checker == nil {
		internalopenai.WriteError(w, http.StatusServiceUnavailable, "service_unavailable", "runtime provider is not configured")
		return
	}

	resp := map[string]any{
		"status": "ready",
		"runtime": map[string]any{
			"retrieval_mode":       info.RetrievalMode,
			"system_config_loaded": info.SystemConfigLoaded,
			"audit_ledger_path":    info.AuditLedgerPath,
			"local_model_required": info.LocalModelRequired,
			"modules_discovered":   info.ModulesDiscovered,
			"modules_enabled":      info.ModulesEnabled,
		},
	}

	readyCtx, cancel := context.WithTimeout(r.Context(), readinessCheckTimeout)
	defer cancel()
	if err := checker.Ready(readyCtx); err != nil {
		resp["status"] = "degraded"
		resp["provider"] = map[string]string{"status": "unavailable", "error": err.Error()}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(resp)
		return
	}
	if ollamaChecker != nil {
		ollamaCtx, cancel := context.WithTimeout(r.Context(), readinessCheckTimeout)
		defer cancel()
		if err := ollamaChecker(ollamaCtx); err != nil {
			resp["local_model"] = map[string]string{"status": "unavailable", "error": err.Error()}
			if info.LocalModelRequired {
				resp["status"] = "degraded"
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
		} else {
			resp["local_model"] = map[string]string{"status": "available"}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
