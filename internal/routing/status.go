package routing

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/rillanai/rillan/internal/config"
	"github.com/rillanai/rillan/internal/providers"
	"github.com/rillanai/rillan/internal/secretstore"
)

type UnavailableReasonCode string

const (
	UnavailableReasonNotConfigured      UnavailableReasonCode = "not_configured"
	UnavailableReasonMissingCredentials UnavailableReasonCode = "missing_credentials"
	UnavailableReasonInvalidCredentials UnavailableReasonCode = "invalid_credentials"
	UnavailableReasonUnsupportedRuntime UnavailableReasonCode = "unsupported_runtime"
	UnavailableReasonNotReady           UnavailableReasonCode = "not_ready"
)

type UnavailableReason struct {
	Code   UnavailableReasonCode
	Detail string
}

type CandidateStatus struct {
	Candidate          Candidate
	Configured         bool
	AuthValid          bool
	Ready              bool
	Available          bool
	UnavailableReasons []UnavailableReason
}

type StatusCatalog struct {
	Candidates []CandidateStatus
	ByID       map[string]CandidateStatus
}

type StatusInput struct {
	Catalog    Catalog
	Config     config.Config
	HTTPClient *http.Client
}

const readinessProbeTimeout = 3 * time.Second

func BuildStatusCatalog(ctx context.Context, input StatusInput) StatusCatalog {
	statuses := make([]CandidateStatus, 0, len(input.Catalog.Candidates))
	for _, candidate := range input.Catalog.Candidates {
		statuses = append(statuses, buildCandidateStatus(ctx, input.Config, candidate, input.HTTPClient))
	}

	slices.SortFunc(statuses, func(left CandidateStatus, right CandidateStatus) int {
		return strings.Compare(left.Candidate.ID, right.Candidate.ID)
	})

	byID := make(map[string]CandidateStatus, len(statuses))
	for _, status := range statuses {
		byID[status.Candidate.ID] = cloneCandidateStatus(status)
	}

	return StatusCatalog{Candidates: statuses, ByID: byID}
}

func buildCandidateStatus(ctx context.Context, cfg config.Config, candidate Candidate, client *http.Client) CandidateStatus {
	status := CandidateStatus{Candidate: cloneCandidate(candidate)}

	providerCfg, err := config.ResolveLLMProviderByID(cfg, candidate.ID)
	if err != nil {
		status.UnavailableReasons = append(status.UnavailableReasons, UnavailableReason{
			Code:   UnavailableReasonNotConfigured,
			Detail: err.Error(),
		})
		return status
	}
	status.Configured = true

	if requiresCredential(providerCfg) && strings.TrimSpace(providerCfg.CredentialRef) == "" {
		status.UnavailableReasons = append(status.UnavailableReasons, UnavailableReason{
			Code:   UnavailableReasonMissingCredentials,
			Detail: fmt.Sprintf("llm provider %q requires credentials", providerCfg.ID),
		})
		return finalizeCandidateStatus(status)
	}

	adapterCfg, err := config.ResolveRuntimeProviderAdapterConfig(cfg, providerCfg)
	if err != nil {
		status.UnavailableReasons = append(status.UnavailableReasons, classifyResolutionError(err))
		return finalizeCandidateStatus(status)
	}
	status.AuthValid = true

	host, err := providers.NewHost(config.RuntimeProviderHostConfig{
		Default:   providerCfg.ID,
		Providers: []config.RuntimeProviderAdapterConfig{adapterCfg},
	}, client)
	if err != nil {
		status.UnavailableReasons = append(status.UnavailableReasons, UnavailableReason{
			Code:   UnavailableReasonUnsupportedRuntime,
			Detail: err.Error(),
		})
		return finalizeCandidateStatus(status)
	}

	provider, err := host.DefaultProvider()
	if err != nil {
		status.UnavailableReasons = append(status.UnavailableReasons, UnavailableReason{
			Code:   UnavailableReasonUnsupportedRuntime,
			Detail: err.Error(),
		})
		return finalizeCandidateStatus(status)
	}

	readyCtx, cancel := context.WithTimeout(ctx, readinessProbeTimeout)
	defer cancel()
	if err := provider.Ready(readyCtx); err != nil {
		status.UnavailableReasons = append(status.UnavailableReasons, UnavailableReason{
			Code:   UnavailableReasonNotReady,
			Detail: err.Error(),
		})
		return finalizeCandidateStatus(status)
	}

	status.Ready = true
	status.Available = true
	return status
}

func finalizeCandidateStatus(status CandidateStatus) CandidateStatus {
	status.Available = status.Configured && status.AuthValid && status.Ready && len(status.UnavailableReasons) == 0
	return status
}

func classifyResolutionError(err error) UnavailableReason {
	if secretstore.IsNotFound(err) {
		return UnavailableReason{Code: UnavailableReasonMissingCredentials, Detail: err.Error()}
	}
	return UnavailableReason{Code: UnavailableReasonInvalidCredentials, Detail: err.Error()}
}

func requiresCredential(provider config.ResolvedLLMProvider) bool {
	return normalizeString(provider.AuthStrategy) != "" && normalizeString(provider.AuthStrategy) != config.AuthStrategyNone
}

func cloneCandidateStatus(status CandidateStatus) CandidateStatus {
	cloned := status
	cloned.Candidate = cloneCandidate(status.Candidate)
	if status.UnavailableReasons != nil {
		cloned.UnavailableReasons = append([]UnavailableReason(nil), status.UnavailableReasons...)
	}
	return cloned
}
