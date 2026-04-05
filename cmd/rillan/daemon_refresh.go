package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rillanai/rillan/internal/config"
	"github.com/rillanai/rillan/internal/httpapi"
)

var daemonRefreshNotifier = notifyDaemonRuntimeRefresh

type daemonRefreshErrorResponse struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func refreshDaemonAfterMutation(cfg config.Config, mutation string) error {
	config.ApplyEnvironmentOverrides(&cfg)
	if err := daemonRefreshNotifier(cfg); err != nil {
		return fmt.Errorf("%s, but %w", mutation, err)
	}
	return nil
}

func notifyDaemonRuntimeRefresh(cfg config.Config) error {
	request, err := http.NewRequest(http.MethodPost, daemonRefreshURL(cfg), nil)
	if err != nil {
		return fmt.Errorf("build daemon refresh request: %w", err)
	}
	if cfg.Server.Auth.Enabled {
		bearer, err := config.ResolveServerAuthBearer(cfg)
		if err != nil {
			return fmt.Errorf("resolve daemon auth bearer: %w", err)
		}
		request.Header.Set("Authorization", "Bearer "+bearer)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		if errors.Is(err, syscall.ECONNREFUSED) {
			return nil
		}
		return fmt.Errorf("notify daemon refresh: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		return nil
	}

	body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	message := strings.TrimSpace(string(body))
	var apiErr daemonRefreshErrorResponse
	if json.Unmarshal(body, &apiErr) == nil && strings.TrimSpace(apiErr.Error.Message) != "" {
		message = strings.TrimSpace(apiErr.Error.Message)
	}
	if message == "" {
		message = response.Status
	}
	return fmt.Errorf("daemon refresh failed: %s", message)
}

func daemonRefreshURL(cfg config.Config) string {
	host := daemonRefreshHost(cfg.Server.Host)
	if host == "" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(cfg.Server.Port)) + httpapi.AdminRuntimeRefreshPath
}

func daemonRefreshHost(host string) string {
	trimmed := strings.TrimSpace(host)
	switch trimmed {
	case "", "0.0.0.0", "::", "[::]":
		return "127.0.0.1"
	default:
		return trimmed
	}
}
