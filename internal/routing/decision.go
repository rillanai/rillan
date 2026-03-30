package routing

import (
	"slices"
	"strings"

	"github.com/sidekickos/rillan/internal/config"
	"github.com/sidekickos/rillan/internal/policy"
)

func ResolvePreference(project config.ProjectConfig, action policy.ActionType) ResolvedPreference {
	if raw, ok := project.Routing.TaskTypes[string(action)]; ok {
		if preference := normalizePreference(raw); preference != "" {
			return ResolvedPreference{Preference: preference, Source: PreferenceSourceTaskType}
		}
	}

	if preference := normalizePreference(project.Routing.Default); preference != "" {
		return ResolvedPreference{Preference: preference, Source: PreferenceSourceProjectDefault}
	}

	return ResolvedPreference{Preference: config.RoutePreferenceAuto, Source: PreferenceSourceDefault}
}

func Decide(input DecisionInput) Decision {
	preference := ResolvePreference(input.Project, input.Action)
	requestedModel := normalizeString(input.RequestedModel)
	requiredCapabilities := normalizeCapabilities(input.RequiredCapabilities)
	states := make([]candidateState, 0, len(input.Candidates))
	for _, candidate := range input.Candidates {
		state := candidateState{candidate: cloneCandidate(candidate)}
		state.trace = CandidateTrace{
			ID:         state.candidate.ID,
			Location:   state.candidate.Location,
			ModelMatch: modelMatches(requestedModel, state.candidate),
		}

		eligible, reason := policyEligible(input.PolicyVerdict, state.candidate)
		if !eligible {
			state.trace.Eligible = false
			state.trace.Rejected = true
			state.trace.Reason = reason
			states = append(states, state)
			continue
		}

		missingCapabilities := missingCapabilities(requiredCapabilities, state.candidate.Capabilities)
		if len(missingCapabilities) > 0 {
			state.trace.Eligible = false
			state.trace.Rejected = true
			state.trace.Reason = "missing_required_capabilities"
			state.trace.MissingCapabilities = missingCapabilities
			states = append(states, state)
			continue
		}

		preferenceScore, preferenceEligible, reason := routePreferenceScore(preference.Preference, state.candidate)
		state.trace.PreferenceScore = preferenceScore
		if !preferenceEligible {
			state.trace.Eligible = false
			state.trace.Rejected = true
			state.trace.Reason = reason
			states = append(states, state)
			continue
		}

		state.trace.Eligible = true
		state.trace.TaskStrength = taskStrength(input.Action, state.candidate.Capabilities)
		states = append(states, state)
	}
	applyModelAffinity(states, requestedModel)

	slices.SortFunc(states, compareCandidateState)

	ranked := make([]Candidate, 0, len(states))
	trace := make([]CandidateTrace, 0, len(states))
	var selected *Candidate
	for i := range states {
		if selected == nil && states[i].trace.Eligible {
			chosen := cloneCandidate(states[i].candidate)
			selected = &chosen
			states[i].trace.Selected = true
		}
		if states[i].trace.Eligible {
			ranked = append(ranked, cloneCandidate(states[i].candidate))
		}
		trace = append(trace, states[i].trace)
	}

	return Decision{
		Preference: preference,
		Selected:   selected,
		Ranked:     ranked,
		Trace: DecisionTrace{
			PolicyVerdict:        input.PolicyVerdict,
			ModelTarget:          input.RequestedModel,
			ModelMatch:           modelMatchLabel(states, requestedModel),
			RequiredCapabilities: requiredCapabilities,
			Preference:           preference.Preference,
			PreferenceSource:     preference.Source,
			Candidates:           trace,
		},
	}
}

type candidateState struct {
	candidate Candidate
	trace     CandidateTrace
}

func compareCandidateState(left candidateState, right candidateState) int {
	if left.trace.Eligible != right.trace.Eligible {
		if left.trace.Eligible {
			return -1
		}
		return 1
	}
	if left.trace.PreferenceScore != right.trace.PreferenceScore {
		if left.trace.PreferenceScore > right.trace.PreferenceScore {
			return -1
		}
		return 1
	}
	if left.trace.ModelMatch != right.trace.ModelMatch {
		if left.trace.ModelMatch {
			return -1
		}
		return 1
	}
	if left.trace.TaskStrength != right.trace.TaskStrength {
		if left.trace.TaskStrength > right.trace.TaskStrength {
			return -1
		}
		return 1
	}
	return strings.Compare(left.candidate.ID, right.candidate.ID)
}

func applyModelAffinity(states []candidateState, requestedModel string) {
	if requestedModel == "" {
		return
	}
	hasMatch := false
	for _, state := range states {
		if state.trace.Eligible && state.trace.ModelMatch {
			hasMatch = true
			break
		}
	}
	if !hasMatch {
		return
	}
	for i := range states {
		if !states[i].trace.Eligible {
			continue
		}
		if states[i].trace.ModelMatch {
			continue
		}
		states[i].trace.Eligible = false
		states[i].trace.Rejected = true
		states[i].trace.Reason = "requested_model_mismatch"
	}
}

func modelMatches(requestedModel string, candidate Candidate) bool {
	if requestedModel == "" {
		return false
	}
	for _, pin := range candidate.ModelPins {
		if normalizeString(pin) == requestedModel {
			return true
		}
	}
	if normalizeString(candidate.DefaultModel) == requestedModel {
		return true
	}
	return false
}

func modelMatchLabel(states []candidateState, requestedModel string) string {
	if requestedModel == "" {
		return "none"
	}
	for _, state := range states {
		if state.trace.ModelMatch {
			return "exact"
		}
	}
	return "none"
}

func policyEligible(verdict policy.Verdict, candidate Candidate) (bool, string) {
	switch verdict {
	case policy.VerdictBlock:
		return false, "policy_blocked"
	case policy.VerdictLocalOnly:
		if candidate.Location != LocationLocal {
			return false, "policy_local_only"
		}
	}
	return true, ""
}

func routePreferenceScore(preference string, candidate Candidate) (int, bool, string) {
	switch normalizePreference(preference) {
	case config.RoutePreferencePreferLocal:
		if candidate.Location == LocationLocal {
			return 1, true, ""
		}
		return 0, true, ""
	case config.RoutePreferencePreferCloud:
		if candidate.Location == LocationRemote {
			return 1, true, ""
		}
		return 0, true, ""
	case config.RoutePreferenceLocalOnly:
		if candidate.Location != LocationLocal {
			return 0, false, "route_preference_local_only"
		}
		return 1, true, ""
	default:
		return 0, true, ""
	}
}

func taskStrength(action policy.ActionType, capabilities []string) int {
	weights := capabilityWeights(action)
	if len(weights) == 0 {
		return 0
	}

	available := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		available[normalizeString(capability)] = struct{}{}
	}

	strength := 0
	for capability, weight := range weights {
		if _, ok := available[capability]; ok {
			strength += weight
		}
	}
	return strength
}

func capabilityWeights(action policy.ActionType) map[string]int {
	switch action {
	case policy.ActionTypeCodeDiagnosis, policy.ActionTypeCodeGeneration, policy.ActionTypeRefactor:
		return map[string]int{"tool_calling": 3, "reasoning": 2, "chat": 1}
	case policy.ActionTypeReview, policy.ActionTypeArchitecture, policy.ActionTypeExplanation:
		return map[string]int{"reasoning": 2, "chat": 1}
	case policy.ActionTypeGeneralQA:
		return map[string]int{"chat": 1}
	default:
		return map[string]int{"chat": 1}
	}
}

func normalizePreference(value string) string {
	switch normalizeString(value) {
	case config.RoutePreferenceAuto:
		return config.RoutePreferenceAuto
	case config.RoutePreferencePreferLocal:
		return config.RoutePreferencePreferLocal
	case config.RoutePreferencePreferCloud:
		return config.RoutePreferencePreferCloud
	case config.RoutePreferenceLocalOnly:
		return config.RoutePreferenceLocalOnly
	default:
		return ""
	}
}

func normalizeString(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func cloneCandidate(candidate Candidate) Candidate {
	cloned := candidate
	if candidate.ModelPins != nil {
		cloned.ModelPins = append([]string(nil), candidate.ModelPins...)
	}
	if candidate.Capabilities != nil {
		cloned.Capabilities = append([]string(nil), candidate.Capabilities...)
	}
	return cloned
}

func normalizeCapabilities(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		normalized := normalizeString(value)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	slices.Sort(result)
	return result
}

func missingCapabilities(required []string, available []string) []string {
	if len(required) == 0 {
		return nil
	}
	availableSet := make(map[string]struct{}, len(available))
	for _, capability := range available {
		availableSet[normalizeString(capability)] = struct{}{}
	}
	missing := make([]string, 0, len(required))
	for _, capability := range required {
		if _, ok := availableSet[capability]; !ok {
			missing = append(missing, capability)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return missing
}
