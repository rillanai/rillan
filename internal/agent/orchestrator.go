package agent

import (
	"strings"

	"github.com/sidekickos/rillan/internal/policy"
)

func DecideExecutionMode(pkg ContextPackage) OrchestrationDecision {
	mode := policy.ExecutionMode(strings.TrimSpace(pkg.Task.ExecutionMode))
	if mode != policy.ExecutionModePlanFirst {
		mode = policy.ExecutionModeDirect
	}

	decision := OrchestrationDecision{
		ExecutionMode: mode,
		Reason:        "execution_mode_default",
	}
	if mode == policy.ExecutionModePlanFirst {
		decision.NextRole = RolePlanner
		decision.Reason = "execution_mode_plan_first"
		return decision
	}

	decision.NextRole = RoleResearcher
	decision.Reason = "execution_mode_direct"
	if strings.TrimSpace(pkg.Task.CurrentStep) != "" {
		decision.NextRole = RoleCoder
		decision.Reason = "execution_mode_direct_current_step"
	}
	return decision
}
