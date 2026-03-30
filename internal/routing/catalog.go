package routing

import (
	"slices"
	"strings"

	"github.com/sidekickos/rillan/internal/config"
)

func BuildCatalog(cfg config.Config, project config.ProjectConfig) Catalog {
	candidates := buildCandidates(cfg)
	allowed := makeAllowlist(project.Providers.LLMAllowed)
	if len(allowed) > 0 {
		filtered := make([]Candidate, 0, len(candidates))
		for _, candidate := range candidates {
			if _, ok := allowed[candidate.ID]; ok {
				filtered = append(filtered, candidate)
			}
		}
		candidates = filtered
	}

	slices.SortFunc(candidates, func(left Candidate, right Candidate) int {
		return strings.Compare(left.ID, right.ID)
	})

	byID := make(map[string]Candidate, len(candidates))
	for _, candidate := range candidates {
		byID[candidate.ID] = cloneCandidate(candidate)
	}

	return Catalog{
		Candidates: candidates,
		ByID:       byID,
		Allowed:    len(allowed) > 0,
	}
}

func buildCandidates(cfg config.Config) []Candidate {
	if cfg.SchemaVersion < config.SchemaVersionV2 || len(cfg.LLMs.Providers) == 0 {
		return []Candidate{legacyCandidate(cfg)}
	}

	candidates := make([]Candidate, 0, len(cfg.LLMs.Providers))
	for _, provider := range cfg.LLMs.Providers {
		family := providerFamily(provider)
		capabilities := providerCapabilities(provider)
		candidates = append(candidates, Candidate{
			ID:           strings.TrimSpace(provider.ID),
			Backend:      family,
			Preset:       strings.TrimSpace(provider.Preset),
			Transport:    strings.TrimSpace(provider.Transport),
			Endpoint:     strings.TrimSpace(provider.Endpoint),
			DefaultModel: strings.TrimSpace(provider.DefaultModel),
			ModelPins:    providerModelPins(provider),
			Capabilities: capabilities,
			Location:     locationForProvider(family, provider.Transport),
		})
	}
	return candidates
}

func legacyCandidate(cfg config.Config) Candidate {
	backend := normalizeString(cfg.Provider.Type)
	return Candidate{
		ID:           "default",
		Backend:      backend,
		Transport:    config.LLMTransportHTTP,
		DefaultModel: "",
		ModelPins:    nil,
		Capabilities: []string{"chat"},
		Location:     locationForProvider(backend, config.LLMTransportHTTP),
	}
}

func providerFamily(provider config.LLMProviderConfig) string {
	if family := normalizeString(provider.Backend); family != "" {
		return family
	}
	if preset := config.BundledLLMProviderPreset(provider.Preset); preset.ID != "" {
		return normalizeString(preset.Family)
	}
	return ""
}

func providerCapabilities(provider config.LLMProviderConfig) []string {
	if len(provider.Capabilities) > 0 {
		return append([]string(nil), provider.Capabilities...)
	}
	if preset := config.BundledLLMProviderPreset(provider.Preset); preset.ID != "" {
		return append([]string(nil), preset.Capabilities...)
	}
	return []string{"chat"}
}

func providerModelPins(provider config.LLMProviderConfig) []string {
	if len(provider.ModelPins) > 0 {
		return append([]string(nil), provider.ModelPins...)
	}
	if preset := config.BundledLLMProviderPreset(provider.Preset); preset.ID != "" && len(preset.ModelPins) > 0 {
		return append([]string(nil), preset.ModelPins...)
	}
	if model := strings.TrimSpace(provider.DefaultModel); model != "" {
		return []string{model}
	}
	return nil
}

func locationForProvider(family string, transport string) Location {
	if normalizeString(transport) == config.LLMTransportSTDIO {
		return LocationLocal
	}
	if normalizeString(family) == config.ProviderOllama {
		return LocationLocal
	}
	return LocationRemote
}

func makeAllowlist(values []string) map[string]struct{} {
	allowed := make(map[string]struct{}, len(values))
	for _, value := range values {
		candidateID := strings.TrimSpace(value)
		if candidateID == "" {
			continue
		}
		allowed[candidateID] = struct{}{}
	}
	return allowed
}
