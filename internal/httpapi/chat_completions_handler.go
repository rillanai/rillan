package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/rillanai/rillan/internal/agent"
	"github.com/rillanai/rillan/internal/audit"
	"github.com/rillanai/rillan/internal/chat"
	"github.com/rillanai/rillan/internal/classify"
	"github.com/rillanai/rillan/internal/config"
	"github.com/rillanai/rillan/internal/observability"
	internalopenai "github.com/rillanai/rillan/internal/openai"
	"github.com/rillanai/rillan/internal/policy"
	"github.com/rillanai/rillan/internal/providers"
	"github.com/rillanai/rillan/internal/retrieval"
	"github.com/rillanai/rillan/internal/routing"
)

type providerHost interface {
	Provider(id string) (providers.Provider, error)
}

type ChatCompletionsHandler struct {
	logger       *slog.Logger
	provider     providers.Provider
	providerHost providerHost
	pipeline     *retrieval.Pipeline
	runtime      RuntimeSnapshotFunc
	project      config.ProjectConfig
	system       *config.SystemConfig
	audit        audit.Recorder
	evaluator    policy.Evaluator
	scanner      *policy.Scanner
	classifier   classify.Classifier
	routeCatalog routing.Catalog
	routeStatus  routing.StatusCatalog
	metrics      *observability.Registry
}

type ChatCompletionsHandlerOption func(*ChatCompletionsHandler)

const (
	headerRetrievalActive    = "X-Rillan-Retrieval"
	headerRetrievalSources   = "X-Rillan-Retrieval-Sources"
	headerRetrievalTopK      = "X-Rillan-Retrieval-Top-K"
	headerRetrievalTruncated = "X-Rillan-Retrieval-Truncated"
	headerRetrievalRefs      = "X-Rillan-Retrieval-Source-Refs"
	maxDebugHeaderSourceRefs = 3
)

func NewChatCompletionsHandler(logger *slog.Logger, provider providers.Provider, pipeline *retrieval.Pipeline, opts ...ChatCompletionsHandlerOption) *ChatCompletionsHandler {
	if logger == nil {
		logger = slog.Default()
	}

	handler := &ChatCompletionsHandler{
		logger:    logger,
		provider:  provider,
		pipeline:  pipeline,
		project:   config.DefaultProjectConfig(),
		evaluator: policy.NewEvaluator(),
		scanner:   policy.DefaultScanner(),
	}
	for _, opt := range opts {
		opt(handler)
	}
	if handler.scanner == nil {
		handler.scanner = policy.DefaultScanner()
	}
	if handler.evaluator == nil {
		handler.evaluator = policy.NewEvaluator()
	}

	return handler
}

func WithProjectConfig(project config.ProjectConfig) ChatCompletionsHandlerOption {
	return func(handler *ChatCompletionsHandler) {
		handler.project = project
	}
}

func WithSystemConfig(system *config.SystemConfig) ChatCompletionsHandlerOption {
	return func(handler *ChatCompletionsHandler) {
		handler.system = system
	}
}

func WithAuditRecorder(recorder audit.Recorder) ChatCompletionsHandlerOption {
	return func(handler *ChatCompletionsHandler) {
		handler.audit = recorder
	}
}

func WithPolicyEvaluator(evaluator policy.Evaluator) ChatCompletionsHandlerOption {
	return func(handler *ChatCompletionsHandler) {
		handler.evaluator = evaluator
	}
}

func WithPolicyScanner(scanner *policy.Scanner) ChatCompletionsHandlerOption {
	return func(handler *ChatCompletionsHandler) {
		handler.scanner = scanner
	}
}

func WithClassifier(classifier classify.Classifier) ChatCompletionsHandlerOption {
	return func(handler *ChatCompletionsHandler) {
		handler.classifier = classifier
	}
}

func WithRuntimeSnapshot(runtime RuntimeSnapshotFunc) ChatCompletionsHandlerOption {
	return func(handler *ChatCompletionsHandler) {
		handler.runtime = runtime
	}
}

func WithProviderHost(host providerHost) ChatCompletionsHandlerOption {
	return func(handler *ChatCompletionsHandler) {
		handler.providerHost = host
	}
}

func WithRouteCatalog(catalog routing.Catalog) ChatCompletionsHandlerOption {
	return func(handler *ChatCompletionsHandler) {
		handler.routeCatalog = catalog
	}
}

func WithRouteStatus(status routing.StatusCatalog) ChatCompletionsHandlerOption {
	return func(handler *ChatCompletionsHandler) {
		handler.routeStatus = status
	}
}

func WithMetrics(metrics *observability.Registry) ChatCompletionsHandlerOption {
	return func(handler *ChatCompletionsHandler) {
		handler.metrics = metrics
	}
}

type chatRuntime struct {
	provider     providers.Provider
	providerHost providerHost
	pipeline     *retrieval.Pipeline
	project      config.ProjectConfig
	system       *config.SystemConfig
	classifier   classify.Classifier
	routeCatalog routing.Catalog
	routeStatus  routing.StatusCatalog
}

func (h *ChatCompletionsHandler) currentRuntime() chatRuntime {
	runtime := chatRuntime{
		provider:     h.provider,
		providerHost: h.providerHost,
		pipeline:     h.pipeline,
		project:      h.project,
		system:       h.system,
		classifier:   h.classifier,
		routeCatalog: h.routeCatalog,
		routeStatus:  h.routeStatus,
	}
	if h.runtime == nil {
		return runtime
	}

	snapshot := h.runtime()
	runtime.provider = snapshot.Provider
	runtime.providerHost = snapshot.ProviderHost
	runtime.pipeline = snapshot.Pipeline
	runtime.project = snapshot.ProjectConfig
	runtime.system = snapshot.SystemConfig
	runtime.classifier = snapshot.Classifier
	runtime.routeCatalog = snapshot.RouteCatalog
	runtime.routeStatus = snapshot.RouteStatus
	return runtime
}

func (h *ChatCompletionsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		internalopenai.WriteError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method must be POST")
		return
	}
	runtime := h.currentRuntime()

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
	if err != nil {
		internalopenai.WriteError(w, http.StatusBadRequest, "invalid_request_error", "request body could not be read")
		return
	}

	var request internalopenai.ChatCompletionRequest
	if err := json.Unmarshal(body, &request); err != nil {
		internalopenai.WriteError(w, http.StatusBadRequest, "invalid_request_error", "request body must be valid JSON")
		return
	}

	if err := internalopenai.ValidateChatCompletionRequest(request); err != nil {
		internalopenai.WriteError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	request, err = applyProjectPromptContext(request, runtime.project)
	if err != nil {
		h.logger.Error("project prompt composition failed", "request_id", RequestIDFromContext(r.Context()), "error", err.Error())
		internalopenai.WriteError(w, http.StatusInternalServerError, "config_error", "project prompt composition failed")
		return
	}
	body, err = json.Marshal(request)
	if err != nil {
		h.logger.Error("request re-encode failed", "request_id", RequestIDFromContext(r.Context()), "error", err.Error())
		internalopenai.WriteError(w, http.StatusInternalServerError, "internal_error", "request could not be prepared")
		return
	}

	outboundRequest := request
	outboundBody := body
	runtimePolicy := policy.MergeRuntimePolicy(runtime.system, runtime.project)
	var classification *policy.IntentClassification
	if runtime.classifier != nil {
		classification, err = runtime.classifier.Classify(r.Context(), request)
		if err != nil {
			h.logger.Warn("intent classification failed", "request_id", RequestIDFromContext(r.Context()), "provider", runtime.provider.Name(), "error", err.Error())
		}
	}
	preflight, err := h.evaluator.Evaluate(r.Context(), policy.EvaluationInput{
		Project:        runtime.project,
		Runtime:        runtimePolicy,
		Request:        request,
		Body:           body,
		Scan:           h.scanner.Scan(body),
		Classification: classification,
		Phase:          policy.EvaluationPhasePreflight,
	})
	if err != nil {
		h.logger.Error("policy preflight failed", "request_id", RequestIDFromContext(r.Context()), "provider", runtime.provider.Name(), "error", err.Error())
		internalopenai.WriteError(w, http.StatusInternalServerError, "policy_error", "policy preflight failed")
		return
	}
	switch preflight.Verdict {
	case policy.VerdictBlock:
		h.recordAudit(r.Context(), audit.Event{
			Type:           audit.EventTypeRemoteDeny,
			RequestID:      RequestIDFromContext(r.Context()),
			Provider:       h.provider.Name(),
			Model:          request.Model,
			Verdict:        string(preflight.Verdict),
			Reason:         preflight.Reason,
			RouteSource:    string(preflight.Trace.RouteSource),
			OutboundSHA256: audit.HashBytes(body),
		})
		internalopenai.WriteError(w, http.StatusForbidden, "policy_violation", "outbound request blocked by policy")
		return
	}
	routeSelection, err := h.resolveRoute(runtime, request.Model, internalopenai.RequiredCapabilities(request), preflight.Verdict, classification)
	h.logRouteDecision(r.Context(), routeSelection)
	if err != nil {
		if preflight.Verdict == policy.VerdictLocalOnly {
			h.recordAudit(r.Context(), audit.Event{
				Type:           audit.EventTypeRemoteDeny,
				RequestID:      RequestIDFromContext(r.Context()),
				Provider:       routeSelection.ProviderKey(),
				Model:          request.Model,
				Verdict:        string(preflight.Verdict),
				Reason:         preflight.Reason,
				RouteSource:    string(preflight.Trace.RouteSource),
				OutboundSHA256: audit.HashBytes(body),
			})
			internalopenai.WriteError(w, http.StatusForbidden, "policy_violation", "request requires local-only handling")
			return
		}
		h.logger.Error("chat route resolution failed", "request_id", RequestIDFromContext(r.Context()), "error", err.Error())
		internalopenai.WriteError(w, http.StatusServiceUnavailable, "upstream_unavailable", "no eligible runtime provider available")
		return
	}
	requestForPreparation := request
	if runtime.pipeline != nil {
		settings, settingsErr := runtime.pipeline.ResolveSettings(request)
		if settingsErr != nil {
			internalopenai.WriteError(w, http.StatusBadRequest, "invalid_request_error", settingsErr.Error())
			return
		}
		if routeSelection.IsRemote() {
			requestForPreparation = applyRetrievalPlan(request, preflight.Retrieval, settings)
		}
	}

	if runtime.pipeline != nil && runtime.pipeline.NeedsPreparation(requestForPreparation) {
		outboundRequest, outboundBody, err = runtime.pipeline.Prepare(r.Context(), requestForPreparation)
		if err != nil {
			h.logger.Error("retrieval preparation failed", "request_id", RequestIDFromContext(r.Context()), "error", err.Error())
			internalopenai.WriteError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
			return
		}
	}
	if metadata, ok := retrieval.ExtractDebugMetadata(outboundRequest); ok {
		summary := retrieval.SummarizeDebug(metadata, maxDebugHeaderSourceRefs)
		applyRetrievalDebugHeaders(w.Header(), summary)
		h.metrics.RecordRetrieval(summary.SourceCount, summary.Truncated)
		h.logger.Info("retrieval context compiled",
			"request_id", RequestIDFromContext(r.Context()),
			"provider", routeSelection.ProviderKey(),
			"top_k", summary.TopK,
			"sources", summary.SourceCount,
			"truncated", summary.Truncated,
			"source_refs", summary.SourceRefs,
		)
	}

	scanResult := h.scanner.Scan(outboundBody)
	evaluation, err := h.evaluator.Evaluate(r.Context(), policy.EvaluationInput{
		Project:        runtime.project,
		Runtime:        runtimePolicy,
		Request:        outboundRequest,
		Body:           outboundBody,
		Scan:           scanResult,
		Classification: classification,
		Phase:          policy.EvaluationPhaseEgress,
	})
	if err != nil {
		h.logger.Error("policy evaluation failed", "request_id", RequestIDFromContext(r.Context()), "provider", routeSelection.ProviderKey(), "error", err.Error())
		internalopenai.WriteError(w, http.StatusInternalServerError, "policy_error", "policy evaluation failed")
		return
	}
	outboundRequest = evaluation.Request
	outboundBody = evaluation.Body

	h.logger.Info("policy evaluated",
		"request_id", RequestIDFromContext(r.Context()),
		"provider", routeSelection.ProviderKey(),
		"verdict", evaluation.Verdict,
		"reason", evaluation.Reason,
		"route_source", evaluation.Trace.RouteSource,
		"findings", len(evaluation.Findings),
	)

	switch evaluation.Verdict {
	case policy.VerdictBlock:
		h.recordAudit(r.Context(), audit.Event{
			Type:           audit.EventTypeRemoteDeny,
			RequestID:      RequestIDFromContext(r.Context()),
			Provider:       routeSelection.ProviderKey(),
			Model:          outboundRequest.Model,
			Verdict:        string(evaluation.Verdict),
			Reason:         evaluation.Reason,
			RouteSource:    string(evaluation.Trace.RouteSource),
			OutboundSHA256: audit.HashBytes(outboundBody),
			SourceRefs:     sourceRefsFromRequest(outboundRequest),
		})
		internalopenai.WriteError(w, http.StatusForbidden, "policy_violation", "outbound request blocked by policy")
		return
	case policy.VerdictLocalOnly:
		if !routeSelection.IsLocal() {
			h.recordAudit(r.Context(), audit.Event{
				Type:           audit.EventTypeRemoteDeny,
				RequestID:      RequestIDFromContext(r.Context()),
				Provider:       routeSelection.ProviderKey(),
				Model:          outboundRequest.Model,
				Verdict:        string(evaluation.Verdict),
				Reason:         evaluation.Reason,
				RouteSource:    string(evaluation.Trace.RouteSource),
				OutboundSHA256: audit.HashBytes(outboundBody),
				SourceRefs:     sourceRefsFromRequest(outboundRequest),
			})
			internalopenai.WriteError(w, http.StatusForbidden, "policy_violation", "request requires local-only handling")
			return
		}
	}

	auditEvent := audit.Event{
		Type:           audit.EventTypeRemoteEgress,
		RequestID:      RequestIDFromContext(r.Context()),
		Provider:       routeSelection.ProviderKey(),
		Model:          outboundRequest.Model,
		Verdict:        string(evaluation.Verdict),
		Reason:         evaluation.Reason,
		RouteSource:    string(evaluation.Trace.RouteSource),
		OutboundSHA256: audit.HashBytes(outboundBody),
		SourceRefs:     sourceRefsFromRequest(outboundRequest),
	}

	response, err := routeSelection.Provider.ChatCompletions(r.Context(), chat.ProviderRequest{Request: outboundRequest, RawBody: outboundBody})
	if err != nil {
		auditEvent.Error = err.Error()
		h.metrics.RecordProviderRequest(routeSelection.ProviderKey(), "error", 0)
		h.recordAudit(r.Context(), auditEvent)
		h.logger.Error("upstream request failed", "request_id", RequestIDFromContext(r.Context()), "provider", routeSelection.ProviderKey(), "error", err.Error())
		status := http.StatusBadGateway
		if isTimeout(err) {
			status = http.StatusGatewayTimeout
		}
		internalopenai.WriteError(w, status, "upstream_error", "upstream request failed")
		return
	}
	defer response.Body.Close()

	copyHeaders(w.Header(), response.Header)
	w.WriteHeader(response.StatusCode)
	auditEvent.ResponseStatus = response.StatusCode

	responseSHA256, err := copyBodyAndHash(w, response.Body, outboundRequest.Stream || strings.Contains(response.Header.Get("Content-Type"), "text/event-stream"))
	auditEvent.ResponseSHA256 = responseSHA256
	if err != nil {
		auditEvent.Error = err.Error()
		h.metrics.RecordProviderRequest(routeSelection.ProviderKey(), "copy_error", response.StatusCode)
		h.recordAudit(r.Context(), auditEvent)
		h.logger.Error("proxy response copy failed", "request_id", RequestIDFromContext(r.Context()), "provider", routeSelection.ProviderKey(), "error", err.Error())
		return
	}
	h.metrics.RecordProviderRequest(routeSelection.ProviderKey(), "success", response.StatusCode)
	h.recordAudit(r.Context(), auditEvent)
}

type chatRouteSelection struct {
	Provider   providers.Provider
	ProviderID string
	Candidate  *routing.Candidate
	Decision   *routing.Decision
}

func (s chatRouteSelection) ProviderKey() string {
	if s.ProviderID != "" {
		return s.ProviderID
	}
	if s.Provider != nil {
		return s.Provider.Name()
	}
	return ""
}

func (s chatRouteSelection) IsLocal() bool {
	return s.Candidate != nil && s.Candidate.Location == routing.LocationLocal
}

func (s chatRouteSelection) IsRemote() bool {
	return s.Candidate == nil || s.Candidate.Location == routing.LocationRemote
}

func (h *ChatCompletionsHandler) resolveRoute(runtime chatRuntime, requestedModel string, requiredCapabilities []string, verdict policy.Verdict, classification *policy.IntentClassification) (chatRouteSelection, error) {
	if runtime.providerHost == nil {
		if verdict == policy.VerdictLocalOnly {
			return chatRouteSelection{Provider: runtime.provider, ProviderID: runtime.provider.Name()}, fmt.Errorf("no local runtime provider available")
		}
		return chatRouteSelection{Provider: runtime.provider, ProviderID: runtime.provider.Name()}, nil
	}

	action := policy.ActionTypeGeneralQA
	if classification != nil && classification.Action != "" {
		action = classification.Action
	}
	decision := routing.Decide(routing.DecisionInput{
		RequestedModel:       requestedModel,
		RequiredCapabilities: requiredCapabilities,
		Action:               action,
		Project:              runtime.project,
		PolicyVerdict:        verdict,
		Candidates:           h.availableRouteCandidates(runtime),
	})
	selection := chatRouteSelection{Decision: &decision}
	if decision.Selected == nil {
		return selection, fmt.Errorf("no eligible runtime provider available")
	}

	provider, err := runtime.providerHost.Provider(decision.Selected.ID)
	if err != nil {
		return selection, err
	}
	selection.Provider = provider
	selection.ProviderID = decision.Selected.ID
	chosen := *decision.Selected
	selection.Candidate = &chosen
	return selection, nil
}

func (h *ChatCompletionsHandler) availableRouteCandidates(runtime chatRuntime) []routing.Candidate {
	if len(runtime.routeStatus.Candidates) > 0 {
		candidates := make([]routing.Candidate, 0, len(runtime.routeStatus.Candidates))
		for _, status := range runtime.routeStatus.Candidates {
			if !status.Available {
				continue
			}
			if runtime.providerHost != nil {
				if _, err := runtime.providerHost.Provider(status.Candidate.ID); err != nil {
					continue
				}
			}
			candidates = append(candidates, status.Candidate)
		}
		return candidates
	}

	if len(runtime.routeCatalog.Candidates) == 0 {
		return nil
	}
	if runtime.providerHost == nil {
		return append([]routing.Candidate(nil), runtime.routeCatalog.Candidates...)
	}

	candidates := make([]routing.Candidate, 0, len(runtime.routeCatalog.Candidates))
	for _, candidate := range runtime.routeCatalog.Candidates {
		if _, err := runtime.providerHost.Provider(candidate.ID); err != nil {
			continue
		}
		candidates = append(candidates, candidate)
	}
	return candidates
}

func (h *ChatCompletionsHandler) logRouteDecision(ctx context.Context, selection chatRouteSelection) {
	if selection.Decision == nil {
		return
	}
	h.logger.Info("chat route selected",
		"request_id", RequestIDFromContext(ctx),
		"provider", selection.ProviderKey(),
		"route_trace", routeTraceLogValue(*selection.Decision, selection.ProviderKey()),
	)
}

func routeTraceLogValue(decision routing.Decision, selectedProviderID string) map[string]any {
	candidates := make([]map[string]any, 0, len(decision.Trace.Candidates))
	for _, candidate := range decision.Trace.Candidates {
		candidates = append(candidates, map[string]any{
			"id":                   candidate.ID,
			"location":             string(candidate.Location),
			"eligible":             candidate.Eligible,
			"rejected":             candidate.Rejected,
			"selected":             candidate.Selected,
			"reason":               candidate.Reason,
			"missing_capabilities": candidate.MissingCapabilities,
			"preference_score":     candidate.PreferenceScore,
			"task_strength":        candidate.TaskStrength,
		})
	}
	return map[string]any{
		"policy_verdict":        string(decision.Trace.PolicyVerdict),
		"model_target":          decision.Trace.ModelTarget,
		"model_match":           decision.Trace.ModelMatch,
		"required_capabilities": decision.Trace.RequiredCapabilities,
		"preference":            decision.Trace.Preference,
		"preference_source":     string(decision.Trace.PreferenceSource),
		"selected_provider_id":  selectedProviderID,
		"candidates":            candidates,
	}
}

func applyProjectPromptContext(req internalopenai.ChatCompletionRequest, project config.ProjectConfig) (internalopenai.ChatCompletionRequest, error) {
	contextMessages, err := buildProjectPromptMessages(project)
	if err != nil {
		return internalopenai.ChatCompletionRequest{}, err
	}
	if len(contextMessages) == 0 {
		return req, nil
	}
	req.Messages = append(contextMessages, req.Messages...)
	return req, nil
}

func buildProjectPromptMessages(project config.ProjectConfig) ([]internalopenai.Message, error) {
	messages := make([]internalopenai.Message, 0, 1+len(project.Agent.Skills.Enabled)+len(project.Instructions))
	if text := strings.TrimSpace(project.SystemPrompt); text != "" {
		messages = append(messages, newSystemMessage(text))
	}
	for _, skillID := range project.Agent.Skills.Enabled {
		skill, err := agent.GetInstalledSkill(skillID)
		if err != nil {
			return nil, err
		}
		content, err := os.ReadFile(skill.ManagedPath)
		if err != nil {
			return nil, fmt.Errorf("read managed skill %q: %w", skill.ID, err)
		}
		text := strings.TrimSpace(string(content))
		if text != "" {
			messages = append(messages, newSystemMessage(text))
		}
	}
	for _, instruction := range project.Instructions {
		if text := strings.TrimSpace(instruction); text != "" {
			messages = append(messages, newSystemMessage(text))
		}
	}
	return messages, nil
}

func newSystemMessage(text string) internalopenai.Message {
	return internalopenai.Message{
		Role:    "system",
		Content: mustMarshalPromptString(text),
	}
}

func mustMarshalPromptString(text string) json.RawMessage {
	data, err := json.Marshal(text)
	if err != nil {
		panic(err)
	}
	return data
}

func (h *ChatCompletionsHandler) recordAudit(ctx context.Context, event audit.Event) {
	if h.audit == nil {
		return
	}
	if err := h.audit.Record(ctx, event); err != nil {
		h.logger.Error("audit record failed", "request_id", RequestIDFromContext(ctx), "error", err.Error())
	}
}

func sourceRefsFromRequest(req internalopenai.ChatCompletionRequest) []string {
	metadata, ok := retrieval.ExtractDebugMetadata(req)
	if !ok {
		return nil
	}
	refs := make([]string, 0, len(metadata.Compiled.Sources))
	for _, source := range metadata.Compiled.Sources {
		refs = append(refs, fmt.Sprintf("%s:%d-%d", source.DocumentPath, source.StartLine, source.EndLine))
	}
	return refs
}

func applyRetrievalPlan(req internalopenai.ChatCompletionRequest, plan policy.RetrievalPlan, settings retrieval.Settings) internalopenai.ChatCompletionRequest {
	if !plan.Apply || !settings.Enabled {
		return req
	}

	cloned := req
	needOverride := (plan.TopKCap > 0 && settings.TopK > plan.TopKCap) || (plan.MaxContextChars > 0 && settings.MaxContextChars > plan.MaxContextChars)
	if !needOverride {
		return cloned
	}
	if cloned.Retrieval == nil {
		cloned.Retrieval = &internalopenai.RetrievalOptions{}
	}
	if plan.TopKCap > 0 && settings.TopK > plan.TopKCap {
		if cloned.Retrieval.TopK == nil || *cloned.Retrieval.TopK > plan.TopKCap {
			topK := plan.TopKCap
			cloned.Retrieval.TopK = &topK
		}
	}
	if plan.MaxContextChars > 0 && settings.MaxContextChars > plan.MaxContextChars {
		if cloned.Retrieval.MaxContextChars == nil || *cloned.Retrieval.MaxContextChars > plan.MaxContextChars {
			maxChars := plan.MaxContextChars
			cloned.Retrieval.MaxContextChars = &maxChars
		}
	}

	return cloned
}

func applyRetrievalDebugHeaders(headers http.Header, summary retrieval.DebugSummary) {
	if !summary.Active {
		return
	}
	headers.Set(headerRetrievalActive, "active")
	headers.Set(headerRetrievalSources, strconv.Itoa(summary.SourceCount))
	headers.Set(headerRetrievalTopK, strconv.Itoa(summary.TopK))
	headers.Set(headerRetrievalTruncated, strconv.FormatBool(summary.Truncated))
	if len(summary.SourceRefs) > 0 {
		headers.Set(headerRetrievalRefs, strings.Join(summary.SourceRefs, "; "))
	}
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func copyHeaders(target, source http.Header) {
	for key, values := range source {
		if isHopByHopHeader(key) {
			continue
		}
		for _, value := range values {
			target.Add(key, value)
		}
	}
}

func isHopByHopHeader(key string) bool {
	switch http.CanonicalHeaderKey(key) {
	case "Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade":
		return true
	default:
		return false
	}
}

func copyBody(w http.ResponseWriter, body io.Reader, streaming bool) error {
	if !streaming {
		_, err := io.Copy(w, body)
		return err
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming response writer does not implement http.Flusher")
	}

	buffer := make([]byte, 32*1024)
	for {
		n, err := body.Read(buffer)
		if n > 0 {
			if _, writeErr := w.Write(buffer[:n]); writeErr != nil {
				return writeErr
			}
			flusher.Flush()
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func copyBodyAndHash(w http.ResponseWriter, body io.Reader, streaming bool) (string, error) {
	hasher := sha256.New()
	tee := io.TeeReader(body, hasher)
	if err := copyBody(w, tee, streaming); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
