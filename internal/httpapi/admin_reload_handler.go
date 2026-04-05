package httpapi

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"

	internalopenai "github.com/rillanai/rillan/internal/openai"
)

const AdminRuntimeRefreshPath = "/admin/runtime/refresh"

func NewAdminReloadHandler(logger *slog.Logger, refresh func(context.Context) error) http.HandlerFunc {
	if logger == nil {
		logger = slog.Default()
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			internalopenai.WriteError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method must be POST")
			return
		}
		if !isLoopbackRequest(r.RemoteAddr) {
			internalopenai.WriteError(w, http.StatusForbidden, "forbidden", "admin refresh is restricted to localhost")
			return
		}
		if refresh == nil {
			internalopenai.WriteError(w, http.StatusServiceUnavailable, "service_unavailable", "runtime refresh is not configured")
			return
		}
		if err := refresh(r.Context()); err != nil {
			logger.Error("runtime refresh failed", "remote_addr", r.RemoteAddr, "error", err.Error())
			internalopenai.WriteError(w, http.StatusInternalServerError, "refresh_error", err.Error())
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func isLoopbackRequest(remoteAddr string) bool {
	host := strings.TrimSpace(remoteAddr)
	if parsedHost, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = parsedHost
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
