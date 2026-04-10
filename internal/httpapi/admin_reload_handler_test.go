// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminReloadHandlerRefreshesFromLoopback(t *testing.T) {
	called := 0
	handler := NewAdminReloadHandler(slog.Default(), func(context.Context) error {
		called++
		return nil
	})
	request := httptest.NewRequest(http.MethodPost, AdminRuntimeRefreshPath, nil)
	request.RemoteAddr = "127.0.0.1:12345"
	recorder := httptest.NewRecorder()

	handler(recorder, request)

	if got, want := recorder.Code, http.StatusNoContent; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if got, want := called, 1; got != want {
		t.Fatalf("refresh calls = %d, want %d", got, want)
	}
}

func TestAdminReloadHandlerRejectsNonLoopbackRequests(t *testing.T) {
	called := 0
	handler := NewAdminReloadHandler(slog.Default(), func(context.Context) error {
		called++
		return nil
	})
	request := httptest.NewRequest(http.MethodPost, AdminRuntimeRefreshPath, nil)
	request.RemoteAddr = "203.0.113.10:12345"
	recorder := httptest.NewRecorder()

	handler(recorder, request)

	if got, want := recorder.Code, http.StatusForbidden; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if got := called; got != 0 {
		t.Fatalf("refresh calls = %d, want 0", got)
	}
}

func TestAdminReloadHandlerSurfacesRefreshFailures(t *testing.T) {
	handler := NewAdminReloadHandler(slog.Default(), func(context.Context) error {
		return errors.New("reload failed")
	})
	request := httptest.NewRequest(http.MethodPost, AdminRuntimeRefreshPath, nil)
	request.RemoteAddr = "[::1]:12345"
	recorder := httptest.NewRecorder()

	handler(recorder, request)

	if got, want := recorder.Code, http.StatusInternalServerError; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if body := recorder.Body.String(); !contains(body, "reload failed") {
		t.Fatalf("body = %q, want refresh error", body)
	}
}
