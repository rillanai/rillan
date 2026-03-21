package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	internalopenai "github.com/sidekickos/rillan/internal/openai"
)

type fakeProvider struct {
	called  int
	request internalopenai.ChatCompletionRequest
	body    []byte
	err     error
	resp    *http.Response
}

func (f *fakeProvider) Name() string                { return "fake" }
func (f *fakeProvider) Ready(context.Context) error { return nil }
func (f *fakeProvider) ChatCompletions(_ context.Context, req internalopenai.ChatCompletionRequest, body []byte) (*http.Response, error) {
	f.called++
	f.request = req
	f.body = append([]byte(nil), body...)
	if f.err != nil {
		return nil, f.err
	}
	if f.resp != nil {
		return f.resp, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"ok"}`)),
	}, nil
}

func TestChatCompletionsHandlerRejectsInvalidRequest(t *testing.T) {
	handler := NewChatCompletionsHandler(slog.Default(), &fakeProvider{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestChatCompletionsHandlerCallsProviderOnce(t *testing.T) {
	provider := &fakeProvider{}
	handler := NewChatCompletionsHandler(slog.Default(), provider)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`))

	handler.ServeHTTP(recorder, request)

	if got, want := provider.called, 1; got != want {
		t.Fatalf("provider calls = %d, want %d", got, want)
	}
	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestChatCompletionsHandlerMapsProviderErrors(t *testing.T) {
	provider := &fakeProvider{err: errors.New("upstream down")}
	handler := NewChatCompletionsHandler(slog.Default(), provider)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`))

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusBadGateway; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}
