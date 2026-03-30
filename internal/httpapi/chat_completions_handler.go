package httpapi

import (
	"context"
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

	"github.com/sidekickos/rillan/internal/agent"
	"github.com/sidekickos/rillan/internal/audit"
	"github.com/sidekickos/rillan/internal/classify"
	"github.com/sidekickos/rillan/internal/config"
	internalopenai "github.com/sidekickos/rillan/internal/openai"
	"github.com/sidekickos/rillan/internal/policy"
	"github.com/sidekickos/rillan/internal/providers"
	"github.com/sidekickos/rillan/internal/retrieval"
)

type ChatCompletionsHandler struct {
	logger     *slog.Logger
	provider   providers.Provider
	pipeline   *retrieval.Pipeline
	project    config.ProjectConfig
	system     *config.SystemConfig
	audit      audit.Recorder
	evaluator  policy.Evaluator
	scanner    *policy.Scanner
	classifier classify.Classifier
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

func (h *ChatCompletionsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		internalopenai.WriteError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method must be POST")
		return
	}

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
	request, err = applyProjectPromptContext(request, h.project)
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
	runtimePolicy := policy.MergeRuntimePolicy(h.system, h.project)
	var classification *policy.IntentClassification
	if h.classifier != nil {
		classification, err = h.classifier.Classify(r.Context(), request)
		if err != nil {
			h.logger.Warn("intent classification failed", "request_id", RequestIDFromContext(r.Context()), "provider", h.provider.Name(), "error", err.Error())
		}
	}
	preflight, err := h.evaluator.Evaluate(r.Context(), policy.EvaluationInput{
		Project:        h.project,
		Runtime:        runtimePolicy,
		Request:        request,
		Body:           body,
		Scan:           h.scanner.Scan(body),
		Classification: classification,
		Phase:          policy.EvaluationPhasePreflight,
	})
	if err != nil {
		h.logger.Error("policy preflight failed", "request_id", RequestIDFromContext(r.Context()), "provider", h.provider.Name(), "error", err.Error())
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
	case policy.VerdictLocalOnly:
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
		internalopenai.WriteError(w, http.StatusForbidden, "policy_violation", "request requires local-only handling")
		return
	}
	requestForPreparation := request
	if h.pipeline != nil {
		settings, settingsErr := h.pipeline.ResolveSettings(request)
		if settingsErr != nil {
			internalopenai.WriteError(w, http.StatusBadRequest, "invalid_request_error", settingsErr.Error())
			return
		}
		requestForPreparation = applyRetrievalPlan(request, preflight.Retrieval, settings)
	}

	if h.pipeline != nil && h.pipeline.NeedsPreparation(requestForPreparation) {
		outboundRequest, outboundBody, err = h.pipeline.Prepare(r.Context(), requestForPreparation)
		if err != nil {
			h.logger.Error("retrieval preparation failed", "request_id", RequestIDFromContext(r.Context()), "error", err.Error())
			internalopenai.WriteError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
			return
		}
	}
	if metadata, ok := retrieval.ExtractDebugMetadata(outboundRequest); ok {
		summary := retrieval.SummarizeDebug(metadata, maxDebugHeaderSourceRefs)
		applyRetrievalDebugHeaders(w.Header(), summary)
		h.logger.Info("retrieval context compiled",
			"request_id", RequestIDFromContext(r.Context()),
			"provider", h.provider.Name(),
			"top_k", summary.TopK,
			"sources", summary.SourceCount,
			"truncated", summary.Truncated,
			"source_refs", summary.SourceRefs,
		)
	}

	scanResult := h.scanner.Scan(outboundBody)
	evaluation, err := h.evaluator.Evaluate(r.Context(), policy.EvaluationInput{
		Project:        h.project,
		Runtime:        runtimePolicy,
		Request:        outboundRequest,
		Body:           outboundBody,
		Scan:           scanResult,
		Classification: classification,
		Phase:          policy.EvaluationPhaseEgress,
	})
	if err != nil {
		h.logger.Error("policy evaluation failed", "request_id", RequestIDFromContext(r.Context()), "provider", h.provider.Name(), "error", err.Error())
		internalopenai.WriteError(w, http.StatusInternalServerError, "policy_error", "policy evaluation failed")
		return
	}
	outboundRequest = evaluation.Request
	outboundBody = evaluation.Body

	h.logger.Info("policy evaluated",
		"request_id", RequestIDFromContext(r.Context()),
		"provider", h.provider.Name(),
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
			Provider:       h.provider.Name(),
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
		h.recordAudit(r.Context(), audit.Event{
			Type:           audit.EventTypeRemoteDeny,
			RequestID:      RequestIDFromContext(r.Context()),
			Provider:       h.provider.Name(),
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

	h.recordAudit(r.Context(), audit.Event{
		Type:           audit.EventTypeRemoteEgress,
		RequestID:      RequestIDFromContext(r.Context()),
		Provider:       h.provider.Name(),
		Model:          outboundRequest.Model,
		Verdict:        string(evaluation.Verdict),
		Reason:         evaluation.Reason,
		RouteSource:    string(evaluation.Trace.RouteSource),
		OutboundSHA256: audit.HashBytes(outboundBody),
		SourceRefs:     sourceRefsFromRequest(outboundRequest),
	})

	response, err := h.provider.ChatCompletions(r.Context(), outboundRequest, outboundBody)
	if err != nil {
		h.logger.Error("upstream request failed", "request_id", RequestIDFromContext(r.Context()), "provider", h.provider.Name(), "error", err.Error())
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

	if err := copyBody(w, response.Body, outboundRequest.Stream || strings.Contains(response.Header.Get("Content-Type"), "text/event-stream")); err != nil {
		h.logger.Error("proxy response copy failed", "request_id", RequestIDFromContext(r.Context()), "provider", h.provider.Name(), "error", err.Error())
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
