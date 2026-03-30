package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sidekickos/rillan/internal/agent"
	"github.com/sidekickos/rillan/internal/audit"
	"github.com/sidekickos/rillan/internal/config"
	"github.com/sidekickos/rillan/internal/index"
	internalopenai "github.com/sidekickos/rillan/internal/openai"
	"github.com/sidekickos/rillan/internal/policy"
	"github.com/sidekickos/rillan/internal/providers"
	"github.com/sidekickos/rillan/internal/retrieval"
	"github.com/sidekickos/rillan/internal/routing"
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

type fakeProviderHost struct {
	providers map[string]providers.Provider
}

func (f fakeProviderHost) Provider(id string) (providers.Provider, error) {
	provider, ok := f.providers[id]
	if !ok {
		return nil, errors.New("provider not found")
	}
	return provider, nil
}

type staticClassifier struct {
	classification *policy.IntentClassification
	err            error
}

func (s staticClassifier) Classify(context.Context, internalopenai.ChatCompletionRequest) (*policy.IntentClassification, error) {
	return s.classification, s.err
}

func TestChatCompletionsHandlerRejectsInvalidRequest(t *testing.T) {
	handler := NewChatCompletionsHandler(slog.Default(), &fakeProvider{}, nil)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestChatCompletionsHandlerCallsProviderOnce(t *testing.T) {
	provider := &fakeProvider{}
	handler := NewChatCompletionsHandler(slog.Default(), provider, nil)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`))

	handler.ServeHTTP(recorder, request)

	if got, want := provider.called, 1; got != want {
		t.Fatalf("provider calls = %d, want %d", got, want)
	}
	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if got, want := string(provider.body), `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`; got != want {
		t.Fatalf("provider body = %s, want %s", got, want)
	}
}

func TestChatCompletionsHandlerRoutesTaskTypePreferenceToLocalCandidate(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	remote := &fakeProvider{}
	local := &fakeProvider{}
	handler := NewChatCompletionsHandler(
		logger,
		remote,
		nil,
		WithClassifier(staticClassifier{classification: &policy.IntentClassification{Action: policy.ActionTypeReview}}),
		WithProjectConfig(config.ProjectConfig{Routing: config.ProjectRoutingConfig{
			Default:   config.RoutePreferencePreferCloud,
			TaskTypes: map[string]string{string(policy.ActionTypeReview): config.RoutePreferencePreferLocal},
		}}),
		WithProviderHost(fakeProviderHost{providers: map[string]providers.Provider{
			"remote-gpt": remote,
			"local-qwen": local,
		}}),
		WithRouteCatalog(routing.Catalog{Candidates: []routing.Candidate{
			{ID: "remote-gpt", Location: routing.LocationRemote, Capabilities: []string{"chat", "reasoning"}},
			{ID: "local-qwen", Location: routing.LocationLocal, Capabilities: []string{"chat", "reasoning"}},
		}}),
		WithRouteStatus(routing.StatusCatalog{Candidates: []routing.CandidateStatus{
			{Candidate: routing.Candidate{ID: "remote-gpt", Location: routing.LocationRemote, Capabilities: []string{"chat", "reasoning"}}, Available: true},
			{Candidate: routing.Candidate{ID: "local-qwen", Location: routing.LocationLocal, Capabilities: []string{"chat", "reasoning"}}, Available: true},
		}}),
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`))

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if got, want := local.called, 1; got != want {
		t.Fatalf("local provider calls = %d, want %d", got, want)
	}
	if got := remote.called; got != 0 {
		t.Fatalf("remote provider calls = %d, want 0", got)
	}
	if !strings.Contains(logs.String(), `"selected_provider_id":"local-qwen"`) {
		t.Fatalf("route trace logs = %s, want selected local provider", logs.String())
	}
	if !strings.Contains(logs.String(), `"preference_source":"task_type"`) {
		t.Fatalf("route trace logs = %s, want task_type preference source", logs.String())
	}
}

func TestChatCompletionsHandlerPreservesStructuredContentAndToolFields(t *testing.T) {
	provider := &fakeProvider{}
	handler := NewChatCompletionsHandler(
		slog.Default(),
		provider,
		nil,
		WithProjectConfig(config.ProjectConfig{SystemPrompt: "System prompt"}),
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":[{"type":"text","text":"ping"}]},{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]}],"tools":[{"type":"function","function":{"name":"lookup","description":"Look up context","parameters":{"type":"object"}}}],"tool_choice":"auto"}`))

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if got, want := provider.called, 1; got != want {
		t.Fatalf("provider calls = %d, want %d", got, want)
	}

	var outbound map[string]any
	if err := json.Unmarshal(provider.body, &outbound); err != nil {
		t.Fatalf("json.Unmarshal(provider.body) returned error: %v", err)
	}
	messages, ok := outbound["messages"].([]any)
	if !ok {
		t.Fatalf("messages type = %T, want []any", outbound["messages"])
	}
	if got, want := len(messages), 3; got != want {
		t.Fatalf("messages length = %d, want %d", got, want)
	}
	first, ok := messages[0].(map[string]any)
	if !ok {
		t.Fatalf("messages[0] type = %T, want map[string]any", messages[0])
	}
	if got, want := first["role"], "system"; got != want {
		t.Fatalf("messages[0].role = %v, want %v", got, want)
	}
	if got, want := first["content"], "System prompt"; got != want {
		t.Fatalf("messages[0].content = %v, want %v", got, want)
	}
	second, ok := messages[1].(map[string]any)
	if !ok {
		t.Fatalf("messages[1] type = %T, want map[string]any", messages[1])
	}
	contentParts, ok := second["content"].([]any)
	if !ok {
		t.Fatalf("messages[1].content type = %T, want []any", second["content"])
	}
	if got, want := len(contentParts), 1; got != want {
		t.Fatalf("messages[1].content length = %d, want %d", got, want)
	}
	part, ok := contentParts[0].(map[string]any)
	if !ok {
		t.Fatalf("messages[1].content[0] type = %T, want map[string]any", contentParts[0])
	}
	if got, want := part["type"], "text"; got != want {
		t.Fatalf("messages[1].content[0].type = %v, want %v", got, want)
	}
	if got, want := part["text"], "ping"; got != want {
		t.Fatalf("messages[1].content[0].text = %v, want %v", got, want)
	}
	third, ok := messages[2].(map[string]any)
	if !ok {
		t.Fatalf("messages[2] type = %T, want map[string]any", messages[2])
	}
	if got := third["content"]; got != nil {
		t.Fatalf("messages[2].content = %v, want nil", got)
	}
	toolCalls, ok := third["tool_calls"].([]any)
	if !ok {
		t.Fatalf("messages[2].tool_calls type = %T, want []any", third["tool_calls"])
	}
	if got, want := len(toolCalls), 1; got != want {
		t.Fatalf("messages[2].tool_calls length = %d, want %d", got, want)
	}
	if got, want := outbound["tool_choice"], "auto"; got != want {
		t.Fatalf("tool_choice = %v, want %v", got, want)
	}
	tools, ok := outbound["tools"].([]any)
	if !ok {
		t.Fatalf("tools type = %T, want []any", outbound["tools"])
	}
	if got, want := len(tools), 1; got != want {
		t.Fatalf("tools length = %d, want %d", got, want)
	}
}

func TestChatCompletionsHandlerInjectsProjectPromptSkillsAndInstructions(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	source := filepath.Join(t.TempDir(), "go-dev.md")
	if err := os.WriteFile(source, []byte("# Go Dev\n\nPrefer small, verifiable Go changes.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if _, err := agent.InstallSkill(source, time.Now()); err != nil {
		t.Fatalf("InstallSkill returned error: %v", err)
	}

	provider := &fakeProvider{}
	handler := NewChatCompletionsHandler(
		slog.Default(),
		provider,
		nil,
		WithProjectConfig(config.ProjectConfig{
			Name:         "demo",
			SystemPrompt: "System prompt",
			Instructions: []string{"Instruction one"},
			Agent:        config.ProjectAgentConfig{Skills: config.ProjectSkillSelectionConfig{Enabled: []string{"go-dev"}}},
		}),
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`))

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if got, want := len(provider.request.Messages), 4; got != want {
		t.Fatalf("messages sent upstream = %d, want %d", got, want)
	}
	for idx, want := range []string{"System prompt", "# Go Dev\n\nPrefer small, verifiable Go changes.", "Instruction one", "ping"} {
		got, err := internalopenai.MessageText(provider.request.Messages[idx])
		if err != nil {
			t.Fatalf("MessageText(%d) returned error: %v", idx, err)
		}
		if got != want {
			t.Fatalf("message[%d] = %q, want %q", idx, got, want)
		}
	}
}

func TestChatCompletionsHandlerMapsProviderErrors(t *testing.T) {
	provider := &fakeProvider{err: errors.New("upstream down")}
	handler := NewChatCompletionsHandler(slog.Default(), provider, nil)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`))

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusBadGateway; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestChatCompletionsHandlerUsesConfigEnabledRetrieval(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")
	seedHandlerStore(t, dbPath, []index.ChunkRecord{{
		ID:           "chunk-1",
		DocumentPath: "docs/context.md",
		Ordinal:      0,
		StartLine:    3,
		EndLine:      4,
		Content:      "provider dispatch should happen after retrieval compilation",
		ContentHash:  "hash-1",
	}})

	provider := &fakeProvider{}
	pipeline := retrieval.NewPipeline(config.RetrievalConfig{Enabled: true, TopK: 1, MaxContextChars: 500}, dbPath)
	handler := NewChatCompletionsHandler(slog.Default(), provider, pipeline)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"tell me about provider dispatch"}]}`))

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if len(provider.request.Messages) != 2 {
		t.Fatalf("messages sent upstream = %d, want 2", len(provider.request.Messages))
	}
	compiled, err := internalopenai.MessageText(provider.request.Messages[0])
	if err != nil {
		t.Fatalf("MessageText returned error: %v", err)
	}
	if !strings.Contains(compiled, "docs/context.md:3-4") {
		t.Fatalf("compiled context missing source reference: %s", compiled)
	}
	if strings.Contains(string(provider.body), `"retrieval"`) {
		t.Fatalf("provider body leaked retrieval field: %s", string(provider.body))
	}
	if got, want := recorder.Header().Get(headerRetrievalActive), "active"; got != want {
		t.Fatalf("%s = %q, want %q", headerRetrievalActive, got, want)
	}
	if got, want := recorder.Header().Get(headerRetrievalSources), "1"; got != want {
		t.Fatalf("%s = %q, want %q", headerRetrievalSources, got, want)
	}
	if got, want := recorder.Header().Get(headerRetrievalTopK), "1"; got != want {
		t.Fatalf("%s = %q, want %q", headerRetrievalTopK, got, want)
	}
	if got := recorder.Header().Get(headerRetrievalRefs); !strings.Contains(got, "docs/context.md:3-4") {
		t.Fatalf("%s = %q, want source ref", headerRetrievalRefs, got)
	}
}

func TestChatCompletionsHandlerRequestOverrideWinsOverConfigDefault(t *testing.T) {
	provider := &fakeProvider{}
	pipeline := retrieval.NewPipeline(config.RetrievalConfig{Enabled: true, TopK: 1, MaxContextChars: 500}, filepath.Join(t.TempDir(), "missing.db"))
	handler := NewChatCompletionsHandler(slog.Default(), provider, pipeline)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}],"retrieval":{"enabled":false}}`))

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if len(provider.request.Messages) != 1 {
		t.Fatalf("messages sent upstream = %d, want 1", len(provider.request.Messages))
	}
	if strings.Contains(string(provider.body), `"retrieval"`) {
		t.Fatalf("provider body leaked retrieval field: %s", string(provider.body))
	}
	if got, want := len(provider.request.Metadata), 0; got != want {
		t.Fatalf("metadata entries = %d, want %d", got, want)
	}
	if got := recorder.Header().Get(headerRetrievalActive); got != "" {
		t.Fatalf("%s = %q, want empty", headerRetrievalActive, got)
	}
}

func TestChatCompletionsHandlerRejectsInvalidRetrievalOverride(t *testing.T) {
	provider := &fakeProvider{}
	pipeline := retrieval.NewPipeline(config.RetrievalConfig{Enabled: false, TopK: 1, MaxContextChars: 500}, filepath.Join(t.TempDir(), "index.db"))
	handler := NewChatCompletionsHandler(slog.Default(), provider, pipeline)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}],"retrieval":{"top_k":0}}`))

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if provider.called != 0 {
		t.Fatalf("provider was called %d times, want 0", provider.called)
	}
}

func TestChatCompletionsHandlerLeavesRetrievalDebugHeadersUnsetWhenInactive(t *testing.T) {
	provider := &fakeProvider{}
	handler := NewChatCompletionsHandler(slog.Default(), provider, nil)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`))

	handler.ServeHTTP(recorder, request)

	for _, header := range []string{headerRetrievalActive, headerRetrievalSources, headerRetrievalTopK, headerRetrievalTruncated, headerRetrievalRefs} {
		if got := recorder.Header().Get(header); got != "" {
			t.Fatalf("%s = %q, want empty", header, got)
		}
	}
}

func TestChatCompletionsHandlerBlocksPolicyViolations(t *testing.T) {
	provider := &fakeProvider{}
	handler := NewChatCompletionsHandler(slog.Default(), provider, nil)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----"}]}`))

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusForbidden; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if got := provider.called; got != 0 {
		t.Fatalf("provider calls = %d, want 0", got)
	}
	if !strings.Contains(recorder.Body.String(), "policy_violation") {
		t.Fatalf("response body = %q, want policy_violation", recorder.Body.String())
	}
}

func TestChatCompletionsHandlerRedactsBeforeDispatch(t *testing.T) {
	provider := &fakeProvider{}
	handler := NewChatCompletionsHandler(slog.Default(), provider, nil)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"token sk-1234567890abcdefghijklmnop"}]}`))

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if got, want := provider.called, 1; got != want {
		t.Fatalf("provider calls = %d, want %d", got, want)
	}
	if strings.Contains(string(provider.body), "sk-1234567890abcdefghijklmnop") {
		t.Fatalf("provider body leaked token: %s", string(provider.body))
	}
	if !strings.Contains(string(provider.body), "[REDACTED OPENAI API KEY]") {
		t.Fatalf("provider body = %s, want redacted token", string(provider.body))
	}
}

func TestChatCompletionsHandlerReturnsLocalOnlyPolicyDenial(t *testing.T) {
	provider := &fakeProvider{}
	handler := NewChatCompletionsHandler(
		slog.Default(),
		provider,
		nil,
		WithProjectConfig(config.ProjectConfig{Name: "demo", Classification: config.ProjectClassificationTradeSecret}),
		WithPolicyEvaluator(policy.NewEvaluator()),
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`))

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusForbidden; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if got := provider.called; got != 0 {
		t.Fatalf("provider calls = %d, want 0", got)
	}
	if !strings.Contains(recorder.Body.String(), "local-only") {
		t.Fatalf("response body = %q, want local-only denial", recorder.Body.String())
	}
}

func TestChatCompletionsHandlerClassifierCanForceLocalOnly(t *testing.T) {
	provider := &fakeProvider{}
	handler := NewChatCompletionsHandler(
		slog.Default(),
		provider,
		nil,
		WithProjectConfig(config.ProjectConfig{Name: "demo", Classification: config.ProjectClassificationInternal}),
		WithClassifier(staticClassifier{classification: &policy.IntentClassification{Sensitivity: policy.SensitivityTradeSecret}}),
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`))

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusForbidden; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if got := provider.called; got != 0 {
		t.Fatalf("provider calls = %d, want 0", got)
	}
}

func TestChatCompletionsHandlerSystemPolicyCanForceLocalOnly(t *testing.T) {
	provider := &fakeProvider{}
	handler := NewChatCompletionsHandler(
		slog.Default(),
		provider,
		nil,
		WithProjectConfig(config.ProjectConfig{Name: "demo", Classification: config.ProjectClassificationOpenSource}),
		WithSystemConfig(&config.SystemConfig{Policy: config.SystemPolicy{Rules: config.SystemPolicyRules{ForceLocalForTradeSecret: true}}}),
		WithClassifier(staticClassifier{classification: &policy.IntentClassification{Sensitivity: policy.SensitivityTradeSecret}}),
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`))

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusForbidden; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if got := provider.called; got != 0 {
		t.Fatalf("provider calls = %d, want 0", got)
	}
}

func TestChatCompletionsHandlerRoutesLocalOnlyPolicyToLocalCandidate(t *testing.T) {
	remote := &fakeProvider{}
	local := &fakeProvider{}
	handler := NewChatCompletionsHandler(
		slog.Default(),
		remote,
		nil,
		WithClassifier(staticClassifier{classification: &policy.IntentClassification{Sensitivity: policy.SensitivityTradeSecret}}),
		WithSystemConfig(&config.SystemConfig{Policy: config.SystemPolicy{Rules: config.SystemPolicyRules{ForceLocalForTradeSecret: true}}}),
		WithProviderHost(fakeProviderHost{providers: map[string]providers.Provider{
			"remote-gpt": remote,
			"local-qwen": local,
		}}),
		WithRouteCatalog(routing.Catalog{Candidates: []routing.Candidate{
			{ID: "remote-gpt", Location: routing.LocationRemote, Capabilities: []string{"chat", "reasoning", "tool_calling"}},
			{ID: "local-qwen", Location: routing.LocationLocal, Capabilities: []string{"chat", "reasoning", "tool_calling"}},
		}}),
		WithRouteStatus(routing.StatusCatalog{Candidates: []routing.CandidateStatus{
			{Candidate: routing.Candidate{ID: "remote-gpt", Location: routing.LocationRemote, Capabilities: []string{"chat", "reasoning", "tool_calling"}}, Available: true},
			{Candidate: routing.Candidate{ID: "local-qwen", Location: routing.LocationLocal, Capabilities: []string{"chat", "reasoning", "tool_calling"}}, Available: true},
		}}),
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`))

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if got, want := local.called, 1; got != want {
		t.Fatalf("local provider calls = %d, want %d", got, want)
	}
	if got := remote.called; got != 0 {
		t.Fatalf("remote provider calls = %d, want 0", got)
	}
}

func TestChatCompletionsHandlerLogsRejectedRouteCandidates(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	remote := &fakeProvider{}
	local := &fakeProvider{}
	handler := NewChatCompletionsHandler(
		logger,
		remote,
		nil,
		WithClassifier(staticClassifier{classification: &policy.IntentClassification{Sensitivity: policy.SensitivityTradeSecret}}),
		WithSystemConfig(&config.SystemConfig{Policy: config.SystemPolicy{Rules: config.SystemPolicyRules{ForceLocalForTradeSecret: true}}}),
		WithProviderHost(fakeProviderHost{providers: map[string]providers.Provider{
			"remote-gpt": remote,
			"local-qwen": local,
		}}),
		WithRouteCatalog(routing.Catalog{Candidates: []routing.Candidate{
			{ID: "remote-gpt", Location: routing.LocationRemote, Capabilities: []string{"chat", "reasoning", "tool_calling"}},
			{ID: "local-qwen", Location: routing.LocationLocal, Capabilities: []string{"chat", "reasoning", "tool_calling"}},
		}}),
		WithRouteStatus(routing.StatusCatalog{Candidates: []routing.CandidateStatus{
			{Candidate: routing.Candidate{ID: "remote-gpt", Location: routing.LocationRemote, Capabilities: []string{"chat", "reasoning", "tool_calling"}}, Available: true},
			{Candidate: routing.Candidate{ID: "local-qwen", Location: routing.LocationLocal, Capabilities: []string{"chat", "reasoning", "tool_calling"}}, Available: true},
		}}),
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`))

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if !strings.Contains(logs.String(), `"id":"remote-gpt"`) {
		t.Fatalf("route trace logs = %s, want remote candidate", logs.String())
	}
	if !strings.Contains(logs.String(), `"rejected":true`) {
		t.Fatalf("route trace logs = %s, want rejected candidate", logs.String())
	}
	if !strings.Contains(logs.String(), `"reason":"policy_local_only"`) {
		t.Fatalf("route trace logs = %s, want policy_local_only rejection", logs.String())
	}
}

func TestChatCompletionsHandlerPreflightCapsRemoteRetrieval(t *testing.T) {
	chunks := make([]index.ChunkRecord, 0, 3)
	for i := 0; i < 3; i++ {
		chunks = append(chunks, index.ChunkRecord{
			ID:           "chunk-" + strconv.Itoa(i),
			DocumentPath: "docs/context-" + strconv.Itoa(i) + ".md",
			Ordinal:      i,
			StartLine:    1,
			EndLine:      2,
			Content:      "retrieval should stay bounded for remote egress",
			ContentHash:  "hash-" + strconv.Itoa(i),
		})
	}
	dbPath := filepath.Join(t.TempDir(), "index.db")
	seedHandlerStore(t, dbPath, chunks)

	provider := &fakeProvider{}
	pipeline := retrieval.NewPipeline(config.RetrievalConfig{Enabled: true, TopK: 4, MaxContextChars: 4000}, dbPath)
	handler := NewChatCompletionsHandler(slog.Default(), provider, pipeline)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"bounded retrieval please"}]}`))

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if got, want := recorder.Header().Get(headerRetrievalTopK), "2"; got != want {
		t.Fatalf("%s = %q, want %q", headerRetrievalTopK, got, want)
	}
	if got, want := recorder.Header().Get(headerRetrievalSources), "2"; got != want {
		t.Fatalf("%s = %q, want %q", headerRetrievalSources, got, want)
	}
}

func TestChatCompletionsHandlerRecordsRemoteEgressAuditEvent(t *testing.T) {
	store, err := audit.NewStore(filepath.Join(t.TempDir(), "audit", "ledger.jsonl"))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}

	provider := &fakeProvider{}
	handler := NewChatCompletionsHandler(
		slog.Default(),
		provider,
		nil,
		WithAuditRecorder(store),
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`))

	handler.ServeHTTP(recorder, request)

	events, err := store.ReadAll(context.Background())
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if got, want := len(events), 1; got != want {
		t.Fatalf("events = %d, want %d", got, want)
	}
	if got, want := events[0].Type, audit.EventTypeRemoteEgress; got != want {
		t.Fatalf("event type = %q, want %q", got, want)
	}
	if events[0].OutboundSHA256 == "" {
		t.Fatal("expected outbound hash to be recorded")
	}
	if strings.Contains(events[0].OutboundSHA256, "ping") {
		t.Fatal("outbound hash should not contain raw payload")
	}
}

func TestChatCompletionsHandlerRecordsRemoteDenyAuditEvent(t *testing.T) {
	store, err := audit.NewStore(filepath.Join(t.TempDir(), "audit", "ledger.jsonl"))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}

	provider := &fakeProvider{}
	handler := NewChatCompletionsHandler(
		slog.Default(),
		provider,
		nil,
		WithAuditRecorder(store),
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----"}]}`))

	handler.ServeHTTP(recorder, request)

	events, err := store.ReadAll(context.Background())
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if got, want := len(events), 1; got != want {
		t.Fatalf("events = %d, want %d", got, want)
	}
	if got, want := events[0].Type, audit.EventTypeRemoteDeny; got != want {
		t.Fatalf("event type = %q, want %q", got, want)
	}
	if got, want := events[0].Verdict, string(policy.VerdictBlock); got != want {
		t.Fatalf("verdict = %q, want %q", got, want)
	}
}

func TestChatCompletionsHandlerBoundsLongRetrievalSourceRefs(t *testing.T) {
	longPath := strings.Repeat("very-long-segment/", 20) + "context.md"
	dbPath := filepath.Join(t.TempDir(), "index.db")
	seedHandlerStore(t, dbPath, []index.ChunkRecord{{
		ID:           "chunk-1",
		DocumentPath: longPath,
		Ordinal:      0,
		StartLine:    12,
		EndLine:      18,
		Content:      "retrieval debug output should stay bounded",
		ContentHash:  "hash-1",
	}})

	provider := &fakeProvider{}
	pipeline := retrieval.NewPipeline(config.RetrievalConfig{Enabled: true, TopK: 1, MaxContextChars: 500}, dbPath)
	handler := NewChatCompletionsHandler(slog.Default(), provider, pipeline)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"bounded debug please"}]}`))

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	got := recorder.Header().Get(headerRetrievalRefs)
	if got == "" {
		t.Fatalf("%s should be set", headerRetrievalRefs)
	}
	if strings.Contains(got, longPath) {
		t.Fatalf("%s leaked full path: %q", headerRetrievalRefs, got)
	}
	if len(got) > 256 {
		t.Fatalf("%s length = %d, want <= 256", headerRetrievalRefs, len(got))
	}
	if !strings.Contains(got, "#") {
		t.Fatalf("%s = %q, want bounded hash marker", headerRetrievalRefs, got)
	}
	if !strings.Contains(got, ":12-18") {
		t.Fatalf("%s = %q, want line range", headerRetrievalRefs, got)
	}
	compiled, err := internalopenai.MessageText(provider.request.Messages[0])
	if err != nil {
		t.Fatalf("MessageText returned error: %v", err)
	}
	if !strings.Contains(compiled, longPath+":12-18") {
		t.Fatalf("compiled context should keep full source attribution, got %q", compiled)
	}
	metadata, ok := retrieval.ExtractDebugMetadata(provider.request)
	if !ok {
		t.Fatal("expected retrieval metadata")
	}
	summary := retrieval.SummarizeDebug(metadata, 1)
	if len(summary.SourceRefs) != 1 {
		t.Fatalf("summary source refs = %d, want 1", len(summary.SourceRefs))
	}
	if len(summary.SourceRefs[0]) > 80 {
		t.Fatalf("summary source ref length = %d, want <= 80", len(summary.SourceRefs[0]))
	}
	if strings.Contains(summary.SourceRefs[0], longPath) {
		t.Fatalf("summary source ref leaked full path: %q", summary.SourceRefs[0])
	}
}

func TestChatCompletionsHandlerBoundsAggregateRetrievalSourceRefsHeader(t *testing.T) {
	chunks := make([]index.ChunkRecord, 0, 5)
	for i := 0; i < 5; i++ {
		chunks = append(chunks, index.ChunkRecord{
			ID:           "chunk-" + strconv.Itoa(i),
			DocumentPath: strings.Repeat("segment/", 15) + "file-" + strconv.Itoa(i) + ".md",
			Ordinal:      i,
			StartLine:    i + 1,
			EndLine:      i + 2,
			Content:      "retrieval aggregate header bound",
			ContentHash:  "hash-" + strconv.Itoa(i),
		})
	}
	dbPath := filepath.Join(t.TempDir(), "index.db")
	seedHandlerStore(t, dbPath, chunks)

	provider := &fakeProvider{}
	pipeline := retrieval.NewPipeline(config.RetrievalConfig{Enabled: true, TopK: 5, MaxContextChars: 1200}, dbPath)
	handler := NewChatCompletionsHandler(slog.Default(), provider, pipeline)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"aggregate bound"}]}`))

	handler.ServeHTTP(recorder, request)

	got := recorder.Header().Get(headerRetrievalRefs)
	if got == "" {
		t.Fatalf("%s should be set", headerRetrievalRefs)
	}
	if len(got) > 256 {
		t.Fatalf("%s length = %d, want <= 256", headerRetrievalRefs, len(got))
	}
}

func seedHandlerStore(t *testing.T, dbPath string, chunks []index.ChunkRecord) {
	t.Helper()

	store, err := index.OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore returned error: %v", err)
	}
	defer store.Close()

	vectors := make([]index.VectorRecord, 0, len(chunks))
	docs := make([]index.DocumentRecord, 0, len(chunks))
	seen := make(map[string]struct{})
	for _, chunk := range chunks {
		if _, ok := seen[chunk.DocumentPath]; !ok {
			docs = append(docs, index.DocumentRecord{Path: chunk.DocumentPath, ContentHash: chunk.DocumentPath, SizeBytes: int64(len(chunk.Content))})
			seen[chunk.DocumentPath] = struct{}{}
		}
		vectors = append(vectors, index.VectorRecord{
			ChunkID:    chunk.ID,
			Dimensions: 8,
			Embedding:  index.EncodeEmbedding(index.PlaceholderEmbedding(chunk.Content)),
		})
	}

	if err := store.ReplaceAll(context.Background(), docs, chunks, vectors); err != nil {
		t.Fatalf("ReplaceAll returned error: %v", err)
	}
}

func TestChatCompletionsHandlerProviderBodyRemainsValidJSON(t *testing.T) {
	provider := &fakeProvider{}
	handler := NewChatCompletionsHandler(slog.Default(), provider, nil)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`))

	handler.ServeHTTP(recorder, request)

	var body map[string]any
	if err := json.Unmarshal(provider.body, &body); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
}
