package agent

import "github.com/sidekickos/rillan/internal/policy"

type Role string

const (
	RoleOrchestrator Role = "orchestrator"
	RolePlanner      Role = "planner"
	RoleResearcher   Role = "researcher"
	RoleCoder        Role = "coder"
	RoleReviewer     Role = "reviewer"
)

type RoleProfile struct {
	Role             Role
	Description      string
	AllowedEffects   []string
	ForbiddenEffects []string
}

func DefaultRoleProfiles() map[Role]RoleProfile {
	return map[Role]RoleProfile{
		RoleOrchestrator: {
			Role:             RoleOrchestrator,
			Description:      "Chooses direct vs planned execution and next-step routing.",
			AllowedEffects:   []string{"read"},
			ForbiddenEffects: []string{"write", "execute"},
		},
		RolePlanner: {
			Role:             RolePlanner,
			Description:      "Converts goals into bounded implementation or research plans.",
			AllowedEffects:   []string{"read"},
			ForbiddenEffects: []string{"write", "execute"},
		},
		RoleResearcher: {
			Role:             RoleResearcher,
			Description:      "Collects repo evidence and index-backed facts.",
			AllowedEffects:   []string{"read"},
			ForbiddenEffects: []string{"write", "execute"},
		},
		RoleCoder: {
			Role:             RoleCoder,
			Description:      "Produces bounded code changes only through later approval-gated actions.",
			AllowedEffects:   []string{"read", "propose_write", "propose_execute"},
			ForbiddenEffects: []string{"write", "execute"},
		},
		RoleReviewer: {
			Role:             RoleReviewer,
			Description:      "Validates work against plan and policy constraints.",
			AllowedEffects:   []string{"read"},
			ForbiddenEffects: []string{"write", "execute"},
		},
	}
}

type OrchestrationDecision struct {
	ExecutionMode policy.ExecutionMode `json:"execution_mode"`
	NextRole      Role                 `json:"next_role"`
	Reason        string               `json:"reason"`
}
