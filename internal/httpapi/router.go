package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/sidekickos/rillan/internal/classify"
	"github.com/sidekickos/rillan/internal/config"
	"github.com/sidekickos/rillan/internal/index"
	"github.com/sidekickos/rillan/internal/policy"
	"github.com/sidekickos/rillan/internal/providers"
	"github.com/sidekickos/rillan/internal/retrieval"
)

// RouterOptions configures the HTTP router.
type RouterOptions struct {
	OllamaChecker   func(context.Context) error
	PipelineOpts    []retrieval.PipelineOption
	ProjectConfig   config.ProjectConfig
	PolicyEvaluator policy.Evaluator
	PolicyScanner   *policy.Scanner
	Classifier      classify.Classifier
}

func NewRouter(logger *slog.Logger, provider providers.Provider, cfg config.Config, opts RouterOptions) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", HealthHandler)
	mux.HandleFunc("GET /readyz", ReadyHandler(provider, opts.OllamaChecker))

	handlerOpts := make([]ChatCompletionsHandlerOption, 0, 4)
	if opts.ProjectConfig.Name != "" {
		handlerOpts = append(handlerOpts, WithProjectConfig(opts.ProjectConfig))
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
