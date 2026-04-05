package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/rillanai/rillan/internal/agent"
	"github.com/rillanai/rillan/internal/audit"
	"github.com/rillanai/rillan/internal/classify"
	"github.com/rillanai/rillan/internal/config"
	"github.com/rillanai/rillan/internal/index"
	"github.com/rillanai/rillan/internal/observability"
	"github.com/rillanai/rillan/internal/policy"
	"github.com/rillanai/rillan/internal/providers"
	"github.com/rillanai/rillan/internal/retrieval"
	"github.com/rillanai/rillan/internal/routing"
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
	RuntimeSnapshot    RuntimeSnapshotFunc
	RefreshRuntime     func(context.Context) error
	Metrics            *observability.Registry
}

func NewRouter(logger *slog.Logger, provider providers.Provider, cfg config.Config, opts RouterOptions) http.Handler {
	mux := http.NewServeMux()
	pipeline := retrieval.NewPipeline(cfg.Retrieval, index.DefaultDBPath(), opts.PipelineOpts...)
	runtimeSnapshot := opts.RuntimeSnapshot
	if runtimeSnapshot == nil {
		snapshot := RuntimeSnapshot{
			Provider:      provider,
			ProviderHost:  opts.ProviderHost,
			Pipeline:      pipeline,
			ProjectConfig: opts.ProjectConfig,
			SystemConfig:  opts.SystemConfig,
			Classifier:    opts.Classifier,
			RouteCatalog:  opts.RouteCatalog,
			RouteStatus:   opts.RouteStatus,
			ReadinessInfo: ReadinessInfo{
				RetrievalMode:      opts.RetrievalMode,
				SystemConfigLoaded: opts.SystemConfigLoaded,
				AuditLedgerPath:    opts.AuditLedgerPath,
				LocalModelRequired: opts.LocalModelRequired,
			},
			OllamaChecker: opts.OllamaChecker,
		}
		runtimeSnapshot = func() RuntimeSnapshot { return snapshot }
	}
	mux.HandleFunc("GET /healthz", HealthHandler)
	mux.HandleFunc("GET /readyz", ReadyHandlerFromRuntime(runtimeSnapshot))
	if opts.Metrics != nil {
		mux.Handle("GET /metrics", protectedHandler(logger, runtimeSnapshot, cfg, opts.Metrics.Handler()))
	}
	if opts.RefreshRuntime != nil {
		mux.Handle("POST "+AdminRuntimeRefreshPath, protectedHandler(logger, runtimeSnapshot, cfg, NewAdminReloadHandler(logger, opts.RefreshRuntime)))
	}
	if cfg.Agent.Enabled {
		gate := agent.NewApprovalGate(opts.AuditRecorder)
		mux.Handle("/v1/agent/tasks", protectedHandler(logger, runtimeSnapshot, cfg, NewAgentTaskHandler(logger, gate, runtimeSnapshot, buildApprovedRepoRoots(cfg))))
		mux.Handle("/v1/agent/proposals/", protectedHandler(logger, runtimeSnapshot, cfg, NewAgentProposalHandler(logger, gate)))
	}

	handlerOpts := make([]ChatCompletionsHandlerOption, 0, 4)
	handlerOpts = append(handlerOpts, WithRuntimeSnapshot(runtimeSnapshot))
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
	if opts.Metrics != nil {
		handlerOpts = append(handlerOpts, WithMetrics(opts.Metrics))
	}

	mux.Handle("/v1/chat/completions", protectedHandler(logger, runtimeSnapshot, cfg, NewChatCompletionsHandler(logger, provider, pipeline, handlerOpts...)))

	return WrapWithMiddleware(logger, opts.Metrics, mux)
}

func buildApprovedRepoRoots(cfg config.Config) []string {
	roots := make([]string, 0, len(cfg.Agent.ApprovedRepoRoots)+1)
	seen := make(map[string]struct{}, len(cfg.Agent.ApprovedRepoRoots)+1)
	appendRoot := func(root string) {
		if root == "" {
			return
		}
		if _, ok := seen[root]; ok {
			return
		}
		seen[root] = struct{}{}
		roots = append(roots, root)
	}
	appendRoot(cfg.Index.Root)
	for _, root := range cfg.Agent.ApprovedRepoRoots {
		appendRoot(root)
	}
	return roots
}
