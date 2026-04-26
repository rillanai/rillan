// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! Orchestrator. Mirrors `internal/agent/orchestrator.go`.

use rillan_policy::ExecutionMode;

use crate::context_package::ContextPackage;
use crate::roles::{ExecutionModeWire, OrchestrationDecision, Role};

/// Decides the next role + execution mode from a context package.
#[must_use]
pub fn decide_execution_mode(pkg: &ContextPackage) -> OrchestrationDecision {
    let mode = match pkg.task.execution_mode.trim() {
        "plan_first" => ExecutionMode::PlanFirst,
        _ => ExecutionMode::Direct,
    };

    if mode == ExecutionMode::PlanFirst {
        return OrchestrationDecision {
            execution_mode: ExecutionModeWire::PlanFirst,
            next_role: Role::Planner,
            reason: "execution_mode_plan_first".into(),
        };
    }

    let mut decision = OrchestrationDecision {
        execution_mode: ExecutionModeWire::Direct,
        next_role: Role::Researcher,
        reason: "execution_mode_direct".into(),
    };
    if !pkg.task.current_step.trim().is_empty() {
        decision.next_role = Role::Coder;
        decision.reason = "execution_mode_direct_current_step".into();
    }
    decision
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::context_package::TaskSection;

    #[test]
    fn plan_first_routes_to_planner() {
        let pkg = ContextPackage {
            task: TaskSection {
                execution_mode: "plan_first".into(),
                ..TaskSection::default()
            },
            ..ContextPackage::default()
        };
        let decision = decide_execution_mode(&pkg);
        assert_eq!(decision.next_role, Role::Planner);
        assert_eq!(decision.reason, "execution_mode_plan_first");
    }

    #[test]
    fn direct_with_current_step_routes_to_coder() {
        let pkg = ContextPackage {
            task: TaskSection {
                execution_mode: "direct".into(),
                current_step: "edit foo.rs".into(),
                ..TaskSection::default()
            },
            ..ContextPackage::default()
        };
        let decision = decide_execution_mode(&pkg);
        assert_eq!(decision.next_role, Role::Coder);
    }

    #[test]
    fn direct_without_step_routes_to_researcher() {
        let pkg = ContextPackage::default();
        let decision = decide_execution_mode(&pkg);
        assert_eq!(decision.next_role, Role::Researcher);
    }
}
