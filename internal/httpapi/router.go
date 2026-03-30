package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/sidekickos/rillan/internal/agent"
	"github.com/sidekickos/rillan/internal/audit"
	"github.com/sidekickos/rillan/internal/classify"
	"github.com/sidekickos/rillan/internal/config"
	"github.com/sidekickos/rillan/internal/index"
	"github.com/sidekickos/rillan/internal/policy"
	"github.com/sidekickos/rillan/internal/providers"
	"github.com/sidekickos/rillan/internal/retrieval"
	"github.com/sidekickos/rillan/internal/routing"
)

// RouterOptions configures the HTTP router.
type RouterOptions struct {
	OllamaChecker      func(context.Context) error
	PipelineOpts       []retrieval.PipelineOption
	ProjectConfig      config.ProjectConfig
	SystemConfig       *config.SystemConfig
	SystemConfigLoaded bool
	AuditLedgerPath    string
	RetrievalMode      string
	LocalModelRequired bool
	AuditRecorder      audit.Recorder
	PolicyEvaluator    policy.Evaluator
	PolicyScanner      *policy.Scanner
	Classifier         classify.Classifier
	ProviderHost       *providers.Host
	RouteCatalog       routing.Catalog
	RouteStatus        routing.StatusCatalog
}

func NewRouter(logger *slog.Logger, provider providers.Provider, cfg config.Config, opts RouterOptions) http.Handler {
	mux := http.NewServeMux()
	gate := agent.NewApprovalGate(opts.AuditRecorder)
	mux.HandleFunc("GET /healthz", HealthHandler)
	mux.HandleFunc("GET /readyz", ReadyHandler(provider, opts.OllamaChecker, ReadinessInfo{
		RetrievalMode:      opts.RetrievalMode,
		SystemConfigLoaded: opts.SystemConfigLoaded,
		AuditLedgerPath:    opts.AuditLedgerPath,
		LocalModelRequired: opts.LocalModelRequired,
	}))
	mux.Handle("/v1/agent/tasks", NewAgentTaskHandler(logger, gate))
	mux.Handle("/v1/agent/proposals/", NewAgentProposalHandler(logger, gate))

	handlerOpts := make([]ChatCompletionsHandlerOption, 0, 4)
	if opts.ProjectConfig.Name != "" {
		handlerOpts = append(handlerOpts, WithProjectConfig(opts.ProjectConfig))
	}
	if opts.SystemConfig != nil {
		handlerOpts = append(handlerOpts, WithSystemConfig(opts.SystemConfig))
	}
	if opts.AuditRecorder != nil {
		handlerOpts = append(handlerOpts, WithAuditRecorder(opts.AuditRecorder))
	}
	if opts.PolicyEvaluator != nil {
		handlerOpts = append(handlerOpts, WithPolicyEvaluator(opts.PolicyEvaluator))
	}
	if opts.PolicyScanner != nil {
		handlerOpts = append(handlerOpts, WithPolicyScanner(opts.PolicyScanner))
	}
	if opts.Classifier != nil {
		handlerOpts = append(handlerOpts, WithClassifier(opts.Classifier))
	}
	if opts.ProviderHost != nil {
		handlerOpts = append(handlerOpts, WithProviderHost(opts.ProviderHost))
	}
	if len(opts.RouteCatalog.Candidates) > 0 {
		handlerOpts = append(handlerOpts, WithRouteCatalog(opts.RouteCatalog))
	}
	if len(opts.RouteStatus.Candidates) > 0 {
		handlerOpts = append(handlerOpts, WithRouteStatus(opts.RouteStatus))
	}

	mux.Handle("/v1/chat/completions", NewChatCompletionsHandler(logger, provider, retrieval.NewPipeline(cfg.Retrieval, index.DefaultDBPath(), opts.PipelineOpts...), handlerOpts...))

	return WrapWithMiddleware(logger, mux)
}
