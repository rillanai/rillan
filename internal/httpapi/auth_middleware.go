// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package httpapi

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"

	"github.com/rillanai/rillan/internal/config"
	internalopenai "github.com/rillanai/rillan/internal/openai"
)

var daemonAuthBearerResolver = config.ResolveServerAuthBearer

func WrapProtectedEndpoint(logger *slog.Logger, cfg config.Config, next http.Handler) http.Handler {
	if !cfg.Server.Auth.Enabled {
		return next
	}
	if logger == nil {
		logger = slog.Default()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided, ok := parseBearerToken(r.Header.Get("Authorization"))
		if !ok {
			writeUnauthorized(w)
			return
		}
		expected, err := daemonAuthBearerResolver(cfg)
		if err != nil {
			logger.Error("server auth resolution failed", "request_id", RequestIDFromContext(r.Context()), "error", err.Error())
			internalopenai.WriteError(w, http.StatusInternalServerError, "config_error", "server auth is misconfigured")
			return
		}
		if subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			writeUnauthorized(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func protectedHandler(logger *slog.Logger, runtimeSnapshot RuntimeSnapshotFunc, fallback config.Config, handler http.Handler) http.Handler {
	if runtimeSnapshot == nil {
		return WrapProtectedEndpoint(logger, fallback, handler)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := runtimeSnapshot().Config
		if cfg.Server.Host == "" {
			cfg = fallback
		}
		WrapProtectedEndpoint(logger, cfg, handler).ServeHTTP(w, r)
	})
}

func parseBearerToken(value string) (string, bool) {
	parts := strings.SplitN(strings.TrimSpace(value), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", false
	}
	return token, true
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="rillan"`)
	internalopenai.WriteError(w, http.StatusUnauthorized, "authentication_error", "missing or invalid bearer token")
}
