package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type testChecker struct {
	err error
}

func (t testChecker) Ready(context.Context) error {
	return t.err
}

func TestHealthHandlerReturnsOK(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	HealthHandler(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestReadyHandlerReturnsServiceUnavailableWhenCheckerFails(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)

	ReadyHandler(testChecker{err: context.DeadlineExceeded}, nil, ReadinessInfo{})(recorder, request)

	if got, want := recorder.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestReadyHandlerIncludesLocalModelStatusWhenAvailable(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)

	ollamaChecker := func(context.Context) error { return nil }
	ReadyHandler(testChecker{}, ollamaChecker, ReadinessInfo{})(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	body := recorder.Body.String()
	if !contains(body, "available") {
		t.Fatalf("expected local_model available in response, got %s", body)
	}
}

func TestReadyHandlerIncludesLocalModelUnavailableStatus(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)

	ollamaChecker := func(context.Context) error { return context.DeadlineExceeded }
	ReadyHandler(testChecker{}, ollamaChecker, ReadinessInfo{})(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d (ollama status should be informational only)", got, want)
	}
	body := recorder.Body.String()
	if !contains(body, "unavailable") {
		t.Fatalf("expected local_model unavailable in response, got %s", body)
	}
}

func TestReadyHandlerOmitsLocalModelWhenNoChecker(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)

	ReadyHandler(testChecker{}, nil, ReadinessInfo{})(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	body := recorder.Body.String()
	if contains(body, `"local_model":`) {
		t.Fatalf("expected no local_model in response when checker is nil, got %s", body)
	}
}

func TestReadyHandlerReturnsServiceUnavailableWhenRequiredLocalModelIsUnavailable(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)

	ollamaChecker := func(context.Context) error { return context.DeadlineExceeded }
	ReadyHandler(testChecker{}, ollamaChecker, ReadinessInfo{LocalModelRequired: true, RetrievalMode: "targeted_remote", SystemConfigLoaded: true, AuditLedgerPath: "/tmp/ledger.jsonl"})(recorder, request)

	if got, want := recorder.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	body := recorder.Body.String()
	for _, want := range []string{"degraded", "targeted_remote", "system_config_loaded", "audit_ledger_path"} {
		if !contains(body, want) {
			t.Fatalf("expected %q in response, got %s", want, body)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
