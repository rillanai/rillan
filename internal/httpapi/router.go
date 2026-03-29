package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/sidekickos/rillan/internal/audit"
	"github.com/sidekickos/rillan/internal/classify"
	"github.com/sidekickos/rillan/internal/config"
	"github.com/sidekickos/rillan/internal/index"
	"github.com/sidekickos/rillan/internal/policy"
	"github.com/sidekickos/rillan/internal/providers"
	"github.com/sidekickos/rillan/internal/retrieval"
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
}

func NewRouter(logger *slog.Logger, provider providers.Provider, cfg config.Config, opts RouterOptions) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", HealthHandler)
	mux.HandleFunc("GET /readyz", ReadyHandler(provider, opts.OllamaChecker, ReadinessInfo{
		RetrievalMode:      opts.RetrievalMode,
		SystemConfigLoaded: opts.SystemConfigLoaded,
		AuditLedgerPath:    opts.AuditLedgerPath,
		LocalModelRequired: opts.LocalModelRequired,
	}))

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

	mux.Handle("/v1/chat/completions", NewChatCompletionsHandler(logger, provider, retrieval.NewPipeline(cfg.Retrieval, index.DefaultDBPath(), opts.PipelineOpts...), handlerOpts...))

	return WrapWithMiddleware(logger, mux)
}
