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

	ReadyHandler(testChecker{err: context.DeadlineExceeded})(recorder, request)

	if got, want := recorder.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}
