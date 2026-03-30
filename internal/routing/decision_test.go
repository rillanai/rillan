package routing

import (
	"testing"

	"github.com/sidekickos/rillan/internal/config"
	"github.com/sidekickos/rillan/internal/policy"
)

func TestResolvePreferenceTaskOverrideWinsOverProjectDefault(t *testing.T) {
	t.Parallel()

	project := config.ProjectConfig{
		Routing: config.ProjectRoutingConfig{
			Default:   config.RoutePreferencePreferCloud,
			TaskTypes: map[string]string{string(policy.ActionTypeReview): config.RoutePreferencePreferLocal},
		},
	}

	resolved := ResolvePreference(project, policy.ActionTypeReview)

	if got, want := resolved.Preference, config.RoutePreferencePreferLocal; got != want {
		t.Fatalf("preference = %q, want %q", got, want)
	}
	if got, want := resolved.Source, PreferenceSourceTaskType; got != want {
		t.Fatalf("source = %q, want %q", got, want)
	}

	resolved = ResolvePreference(project, policy.ActionTypeArchitecture)
	if got, want := resolved.Preference, config.RoutePreferencePreferCloud; got != want {
		t.Fatalf("default preference = %q, want %q", got, want)
	}
	if got, want := resolved.Source, PreferenceSourceProjectDefault; got != want {
		t.Fatalf("default source = %q, want %q", got, want)
	}
}

func TestDecidePolicyLocalOnlyExcludesRemote(t *testing.T) {
	t.Parallel()

	decision := Decide(DecisionInput{
		Action:        policy.ActionTypeCodeGeneration,
		Project:       config.ProjectConfig{Routing: config.ProjectRoutingConfig{Default: config.RoutePreferencePreferCloud}},
		PolicyVerdict: policy.VerdictLocalOnly,
		Candidates: []Candidate{
			{ID: "remote-a", Location: LocationRemote, Capabilities: []string{"chat", "reasoning", "tool_calling"}},
			{ID: "local-a", Location: LocationLocal, Capabilities: []string{"chat", "reasoning", "tool_calling"}},
		},
	})

	if decision.Selected == nil {
		t.Fatal("expected selected candidate")
	}
	if got, want := decision.Selected.ID, "local-a"; got != want {
		t.Fatalf("selected id = %q, want %q", got, want)
	}

	rejected := findTraceCandidate(t, decision.Trace.Candidates, "remote-a")
	if rejected.Eligible {
		t.Fatal("expected remote candidate to be ineligible")
	}
	if got, want := rejected.Reason, "policy_local_only"; got != want {
		t.Fatalf("remote rejection reason = %q, want %q", got, want)
	}
	if got, want := decision.Trace.PolicyVerdict, policy.VerdictLocalOnly; got != want {
		t.Fatalf("policy verdict = %q, want %q", got, want)
	}
}

func TestDecideUsesStableTieBreakByProviderID(t *testing.T) {
	t.Parallel()

	decision := Decide(DecisionInput{
		Action:        policy.ActionTypeGeneralQA,
		Project:       config.ProjectConfig{Routing: config.ProjectRoutingConfig{Default: config.RoutePreferenceAuto}},
		PolicyVerdict: policy.VerdictAllow,
		Candidates: []Candidate{
			{ID: "zeta", Location: LocationRemote, Capabilities: []string{"chat"}},
			{ID: "alpha", Location: LocationRemote, Capabilities: []string{"chat"}},
		},
	})

	if decision.Selected == nil {
		t.Fatal("expected selected candidate")
	}
	if got, want := decision.Selected.ID, "alpha"; got != want {
		t.Fatalf("selected id = %q, want %q", got, want)
	}
	if got, want := decision.Ranked[0].ID, "alpha"; got != want {
		t.Fatalf("ranked[0] id = %q, want %q", got, want)
	}
	if got, want := decision.Ranked[1].ID, "zeta"; got != want {
		t.Fatalf("ranked[1] id = %q, want %q", got, want)
	}
}

func TestDecideTraceIncludesRejectedCandidates(t *testing.T) {
	t.Parallel()

	decision := Decide(DecisionInput{
		Action:        policy.ActionTypeReview,
		Project:       config.ProjectConfig{Routing: config.ProjectRoutingConfig{Default: config.RoutePreferenceLocalOnly}},
		PolicyVerdict: policy.VerdictAllow,
		Candidates: []Candidate{
			{ID: "remote-a", Location: LocationRemote, Capabilities: []string{"chat", "reasoning"}},
			{ID: "local-a", Location: LocationLocal, Capabilities: []string{"chat", "reasoning"}},
		},
	})

	if got, want := len(decision.Trace.Candidates), 2; got != want {
		t.Fatalf("trace candidate count = %d, want %d", got, want)
	}

	rejected := findTraceCandidate(t, decision.Trace.Candidates, "remote-a")
	if rejected.Eligible {
		t.Fatal("expected remote candidate to be rejected by route preference")
	}
	if got, want := rejected.Reason, "route_preference_local_only"; got != want {
		t.Fatalf("remote rejection reason = %q, want %q", got, want)
	}
	if !rejected.Rejected {
		t.Fatal("expected rejected flag to be true")
	}
	selected := findTraceCandidate(t, decision.Trace.Candidates, "local-a")
	if !selected.Selected {
		t.Fatal("expected local candidate to be marked selected")
	}
}

func TestDecideRequestedModelExactMatchRestrictsCandidates(t *testing.T) {
	t.Parallel()

	decision := Decide(DecisionInput{
		RequestedModel: "claude-sonnet-4-5",
		Action:         policy.ActionTypeReview,
		Project:        config.ProjectConfig{Routing: config.ProjectRoutingConfig{Default: config.RoutePreferencePreferLocal}},
		PolicyVerdict:  policy.VerdictAllow,
		Candidates: []Candidate{
			{ID: "local-qwen", Location: LocationLocal, DefaultModel: "qwen3:8b", ModelPins: []string{"qwen3:8b"}, Capabilities: []string{"chat", "reasoning"}},
			{ID: "claude-prod", Location: LocationRemote, DefaultModel: "claude-sonnet-4-5", ModelPins: []string{"claude-sonnet-4-5"}, Capabilities: []string{"chat", "reasoning"}},
		},
	})

	if decision.Selected == nil {
		t.Fatal("expected selected candidate")
	}
	if got, want := decision.Selected.ID, "claude-prod"; got != want {
		t.Fatalf("selected id = %q, want %q", got, want)
	}
	if got, want := decision.Trace.ModelMatch, "exact"; got != want {
		t.Fatalf("model match = %q, want %q", got, want)
	}
	rejected := findTraceCandidate(t, decision.Trace.Candidates, "local-qwen")
	if got, want := rejected.Reason, "requested_model_mismatch"; got != want {
		t.Fatalf("rejection reason = %q, want %q", got, want)
	}
}

func TestDecideRequestedModelNoMatchFallsBackToExistingOrder(t *testing.T) {
	t.Parallel()

	decision := Decide(DecisionInput{
		RequestedModel: "unknown-model",
		Action:         policy.ActionTypeGeneralQA,
		Project:        config.ProjectConfig{Routing: config.ProjectRoutingConfig{Default: config.RoutePreferenceAuto}},
		PolicyVerdict:  policy.VerdictAllow,
		Candidates: []Candidate{
			{ID: "alpha", Location: LocationRemote, DefaultModel: "gpt-5", ModelPins: []string{"gpt-5"}, Capabilities: []string{"chat"}},
			{ID: "zeta", Location: LocationRemote, DefaultModel: "claude-sonnet-4-5", ModelPins: []string{"claude-sonnet-4-5"}, Capabilities: []string{"chat"}},
		},
	})

	if decision.Selected == nil {
		t.Fatal("expected selected candidate")
	}
	if got, want := decision.Selected.ID, "alpha"; got != want {
		t.Fatalf("selected id = %q, want %q", got, want)
	}
	if got, want := decision.Trace.ModelMatch, "none"; got != want {
		t.Fatalf("model match = %q, want %q", got, want)
	}
}

func TestDecideRequestedModelRespectsLocalOnlyWithinMatchedSubset(t *testing.T) {
	t.Parallel()

	decision := Decide(DecisionInput{
		RequestedModel: "qwen3:8b",
		Action:         policy.ActionTypeCodeGeneration,
		Project:        config.ProjectConfig{Routing: config.ProjectRoutingConfig{Default: config.RoutePreferencePreferCloud}},
		PolicyVerdict:  policy.VerdictLocalOnly,
		Candidates: []Candidate{
			{ID: "remote-qwen", Location: LocationRemote, DefaultModel: "qwen3:8b", ModelPins: []string{"qwen3:8b"}, Capabilities: []string{"chat", "reasoning", "tool_calling"}},
			{ID: "local-qwen", Location: LocationLocal, DefaultModel: "qwen3:8b", ModelPins: []string{"qwen3:8b"}, Capabilities: []string{"chat", "reasoning", "tool_calling"}},
		},
	})

	if decision.Selected == nil {
		t.Fatal("expected selected candidate")
	}
	if got, want := decision.Selected.ID, "local-qwen"; got != want {
		t.Fatalf("selected id = %q, want %q", got, want)
	}
	rejected := findTraceCandidate(t, decision.Trace.Candidates, "remote-qwen")
	if got, want := rejected.Reason, "policy_local_only"; got != want {
		t.Fatalf("rejection reason = %q, want %q", got, want)
	}
}

func TestDecideRequiredCapabilitiesExcludeCandidatesMissingTools(t *testing.T) {
	t.Parallel()

	decision := Decide(DecisionInput{
		RequestedModel:       "gpt-5",
		RequiredCapabilities: []string{"tool_calling"},
		Action:               policy.ActionTypeCodeGeneration,
		Project:              config.ProjectConfig{Routing: config.ProjectRoutingConfig{Default: config.RoutePreferenceAuto}},
		PolicyVerdict:        policy.VerdictAllow,
		Candidates: []Candidate{
			{ID: "chat-only", Location: LocationRemote, ModelPins: []string{"gpt-5"}, Capabilities: []string{"chat", "reasoning"}},
			{ID: "tool-capable", Location: LocationRemote, ModelPins: []string{"gpt-5"}, Capabilities: []string{"chat", "reasoning", "tool_calling"}},
		},
	})

	if decision.Selected == nil || decision.Selected.ID != "tool-capable" {
		t.Fatalf("selected = %#v, want tool-capable", decision.Selected)
	}
	rejected := findTraceCandidate(t, decision.Trace.Candidates, "chat-only")
	if got, want := rejected.Reason, "missing_required_capabilities"; got != want {
		t.Fatalf("rejection reason = %q, want %q", got, want)
	}
	if got, want := rejected.MissingCapabilities, []string{"tool_calling"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("missing capabilities = %#v, want %#v", got, want)
	}
}

func TestDecideRequiredCapabilitiesMultimodalRejectsChatOnly(t *testing.T) {
	t.Parallel()

	decision := Decide(DecisionInput{
		RequiredCapabilities: []string{"multimodal"},
		Action:               policy.ActionTypeGeneralQA,
		Project:              config.ProjectConfig{Routing: config.ProjectRoutingConfig{Default: config.RoutePreferenceAuto}},
		PolicyVerdict:        policy.VerdictAllow,
		Candidates: []Candidate{
			{ID: "chat-only", Location: LocationRemote, Capabilities: []string{"chat"}},
			{ID: "vision", Location: LocationRemote, Capabilities: []string{"chat", "multimodal"}},
		},
	})

	if decision.Selected == nil || decision.Selected.ID != "vision" {
		t.Fatalf("selected = %#v, want vision", decision.Selected)
	}
}

func findTraceCandidate(t *testing.T, traces []CandidateTrace, id string) CandidateTrace {
	t.Helper()

	for _, candidate := range traces {
		if candidate.ID == id {
			return candidate
		}
	}

	t.Fatalf("trace candidate %q not found", id)
	return CandidateTrace{}
}
