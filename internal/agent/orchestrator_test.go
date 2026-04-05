package agent

import (
	"testing"

	"github.com/rillanai/rillan/internal/policy"
)

func TestDecideExecutionModeChoosesPlannerForPlanFirst(t *testing.T) {
	decision := DecideExecutionMode(ContextPackage{Task: TaskSection{ExecutionMode: string(policy.ExecutionModePlanFirst)}})
	if got, want := decision.NextRole, RolePlanner; got != want {
		t.Fatalf("next role = %q, want %q", got, want)
	}
	if got, want := decision.ExecutionMode, policy.ExecutionModePlanFirst; got != want {
		t.Fatalf("execution mode = %q, want %q", got, want)
	}
}

func TestDecideExecutionModeDefaultsToResearcherForDirect(t *testing.T) {
	decision := DecideExecutionMode(ContextPackage{Task: TaskSection{ExecutionMode: string(policy.ExecutionModeDirect)}})
	if got, want := decision.NextRole, RoleResearcher; got != want {
		t.Fatalf("next role = %q, want %q", got, want)
	}
}

func TestDecideExecutionModeUsesCoderWhenCurrentStepExists(t *testing.T) {
	decision := DecideExecutionMode(ContextPackage{Task: TaskSection{ExecutionMode: string(policy.ExecutionModeDirect), CurrentStep: "apply patch"}})
	if got, want := decision.NextRole, RoleCoder; got != want {
		t.Fatalf("next role = %q, want %q", got, want)
	}
}
