# Audit Execution Backlog

This backlog translates the completed audit into executable tickets.

## Release 1 — Boundary hardening

### T001 — Gate agent routes on `agent.enabled`
- **Priority:** P0
- **Status:** Done
- **Why:** Agent routes are currently mounted even when `agent.enabled` defaults to false.
- **Scope:** `internal/httpapi/router.go`, router-level tests.
- **Acceptance criteria:**
  - `/v1/agent/tasks` and `/v1/agent/proposals/*` are not registered when `cfg.Agent.Enabled` is false.
  - Those routes are registered when `cfg.Agent.Enabled` is true.
  - Regression tests cover both enabled and disabled behavior.
- **Dependencies:** None

### T002 — Add auth/authz middleware for non-health endpoints
- **Priority:** P0
- **Status:** Done
- **Why:** Chat and agent APIs currently rely on network placement instead of endpoint auth.
- **Scope:** HTTP middleware and route protection for chat, agent, and admin endpoints.
- **Acceptance criteria:**
  - Non-health endpoints reject unauthorized requests.
  - Health endpoints remain accessible without auth.
  - Auth failure behavior is documented and tested.
- **Dependencies:** T001

### T003 — Enforce safer non-loopback bind behavior
- **Priority:** P0
- **Status:** Done
- **Why:** Current config allows non-loopback binds without requiring explicit hardening.
- **Scope:** Config validation/startup safeguards and docs.
- **Acceptance criteria:**
  - Non-loopback binds require explicit opt-in.
  - Unsafe exposure paths are documented.
- **Dependencies:** T002

### T004 — Constrain agent repo roots to approved roots
- **Priority:** P0
- **Status:** Done
- **Why:** Caller-controlled `repo_root` expands trust boundaries too far.
- **Scope:** Agent task handling, runner, skill registry.
- **Acceptance criteria:**
  - Arbitrary repo roots are rejected.
  - Approved roots remain usable.
  - Regression tests cover rejection paths.
- **Dependencies:** T001

### T005 — Prevent symlink escape in read/search helpers
- **Priority:** P0
- **Status:** Done
- **Why:** Prefix-based path checks can be bypassed through symlinks.
- **Scope:** `internal/agent/skills/read_only.go` plus tests.
- **Acceptance criteria:**
  - Resolved paths outside repo root are rejected.
  - Search helpers do not traverse escaped symlink targets.
- **Dependencies:** T004

### T006 — Separate module discovery from activation
- **Priority:** P0
- **Status:** Done
- **Why:** Repo-local modules can currently shape runtime behavior too easily.
- **Scope:** Module loading and runtime snapshot activation policy.
- **Acceptance criteria:**
  - Discovery does not imply activation.
  - Executable/runtime module behavior requires explicit trust.
- **Dependencies:** None

### T007 — Add explicit trust policy for repo-local modules
- **Priority:** P0
- **Status:** Done
- **Why:** HTTP/stdio module adapters expand outbound and subprocess attack surface.
- **Scope:** Module config/trust enforcement and docs.
- **Acceptance criteria:**
  - Module activation requires explicit trust state.
  - Stdio module execution is gated behind stronger opt-in.
- **Dependencies:** T006

## Release 2 — Operational correctness

### T008 — Add server timeouts
- **Priority:** P1
- **Status:** Done
- **Scope:** `internal/app/app.go`
- **Acceptance criteria:** Read, write, idle, and header timeouts are configured and tested.
- **Dependencies:** T001

### T009 — Add outbound provider and Ollama client timeouts
- **Priority:** P1
- **Status:** Done
- **Scope:** Provider/Ollama client construction.
- **Acceptance criteria:** Upstream hangs fail within bounded time.
- **Dependencies:** T008

### T010 — Implement real provider readiness checks
- **Priority:** P1
- **Status:** Done
- **Scope:** Provider `Ready()` behavior and route status evaluation.
- **Acceptance criteria:** Remote readiness reflects actual upstream state.
- **Dependencies:** T009

### T011 — Make `/readyz` reflect degraded upstream state
- **Priority:** P1
- **Status:** Done
- **Scope:** Readiness reporting.
- **Acceptance criteria:** Upstream failures surface in readiness responses and status.
- **Dependencies:** T010

### T012 — Record response-side audit metadata
- **Priority:** P1
- **Status:** Done
- **Scope:** Chat handler + audit event population.
- **Acceptance criteria:** Response status/hash/error path is recorded for success and failure.
- **Dependencies:** T010

## Release 3 — Observability

### T013 — Add core request/provider/retrieval metrics
- **Priority:** P1
- **Status:** Done

### T014 — Add correlation propagation on outbound requests
- **Priority:** P1
- **Status:** Done

### T015 — Improve shutdown and readiness transition logging
- **Priority:** P2
- **Status:** Done

### T016 — Document logs, audit ledger, and readiness triage
- **Priority:** P2
- **Status:** Done

## Release 4 — Boundary cleanup

### T017 — Introduce transport DTOs for agent endpoints
- **Priority:** P1
- **Status:** Done

### T018 — Map HTTP DTOs to internal agent/runtime structs
- **Priority:** P1
- **Status:** Done

### T019 — Reduce `internal/openai` transport DTO leakage across layers
- **Priority:** P2
- **Status:** Done

### T020 — Narrow provider interface toward domain-facing request types
- **Priority:** P2
- **Status:** Done

## Delivery order
1. Boundary hardening
2. Operational correctness
3. Observability
4. Boundary cleanup
