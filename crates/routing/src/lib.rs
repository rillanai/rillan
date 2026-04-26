// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! Deterministic routing decision engine. Mirrors `internal/routing` from the
//! Go repo. ADR-005.
//!
//! The catalog enumerates candidate providers from the runtime config (with
//! optional project allowlist filtering); the decision engine ranks them
//! against the request's policy verdict, requested model, required
//! capabilities, and the project's routing preferences.

mod catalog;
mod decision;
mod status;
mod types;

pub use catalog::build_catalog;
pub use decision::{decide, resolve_preference};
pub use status::{
    build_status_catalog, CandidateStatus, StatusCatalog, StatusInput, UnavailableReason,
    UnavailableReasonCode,
};
pub use types::{
    Candidate, CandidateTrace, Catalog, Decision, DecisionInput, DecisionTrace, Location,
    PreferenceSource, ResolvedPreference,
};
