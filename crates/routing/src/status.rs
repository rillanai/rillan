// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! Status catalog. Mirrors `internal/routing/status.go`.
//!
//! Each candidate is run through the same configuration + secret-resolution +
//! provider-host pipeline the daemon uses for live traffic, then a bounded
//! readiness probe is dispatched. Results bubble up as a structured
//! [`StatusCatalog`] for the `/readyz` and CLI status surfaces.

use std::collections::BTreeMap;
use std::time::Duration;

use rillan_config::{
    resolve_llm_provider_by_id, resolve_runtime_provider_adapter, Config, ResolvedLlmProvider,
    RuntimeProviderHostConfig, AUTH_STRATEGY_NONE,
};
use rillan_providers::Host;
use rillan_secretstore::Store;
use serde::{Deserialize, Serialize};

use crate::types::{Candidate, Catalog};

/// Top-level reason code for a candidate that is not currently usable.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum UnavailableReasonCode {
    NotConfigured,
    MissingCredentials,
    InvalidCredentials,
    UnsupportedRuntime,
    NotReady,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct UnavailableReason {
    pub code: UnavailableReasonCode,
    pub detail: String,
}

#[derive(Debug, Clone, PartialEq)]
pub struct CandidateStatus {
    pub candidate: Candidate,
    pub configured: bool,
    pub auth_valid: bool,
    pub ready: bool,
    pub available: bool,
    pub unavailable_reasons: Vec<UnavailableReason>,
}

/// Materialized view of every candidate's readiness.
#[derive(Debug, Clone, Default)]
pub struct StatusCatalog {
    pub candidates: Vec<CandidateStatus>,
    pub by_id: BTreeMap<String, CandidateStatus>,
}

/// Inputs to [`build_status_catalog`].
pub struct StatusInput<'a> {
    pub catalog: Catalog,
    pub config: &'a Config,
    pub store: Store,
}

impl std::fmt::Debug for StatusInput<'_> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("StatusInput")
            .field("catalog", &self.catalog)
            .finish_non_exhaustive()
    }
}

const READINESS_PROBE_TIMEOUT: Duration = Duration::from_secs(3);

/// Builds a [`StatusCatalog`] by walking every candidate and running the same
/// resolution + readiness checks the live request path uses.
pub async fn build_status_catalog(input: StatusInput<'_>) -> StatusCatalog {
    let mut statuses = Vec::with_capacity(input.catalog.candidates.len());
    for candidate in input.catalog.candidates {
        statuses.push(build_candidate_status(input.config, &input.store, candidate).await);
    }
    statuses.sort_by(|a, b| a.candidate.id.cmp(&b.candidate.id));
    let by_id: BTreeMap<String, CandidateStatus> = statuses
        .iter()
        .map(|status| (status.candidate.id.clone(), status.clone()))
        .collect();
    StatusCatalog {
        candidates: statuses,
        by_id,
    }
}

async fn build_candidate_status(
    cfg: &Config,
    store: &Store,
    candidate: Candidate,
) -> CandidateStatus {
    let mut status = CandidateStatus {
        candidate: candidate.clone(),
        configured: false,
        auth_valid: false,
        ready: false,
        available: false,
        unavailable_reasons: Vec::new(),
    };

    let provider_cfg = match resolve_llm_provider_by_id(cfg, &candidate.id) {
        Ok(value) => value,
        Err(err) => {
            status.unavailable_reasons.push(UnavailableReason {
                code: UnavailableReasonCode::NotConfigured,
                detail: err.to_string(),
            });
            return finalize(status);
        }
    };
    status.configured = true;

    if requires_credential(&provider_cfg) && provider_cfg.credential_ref.trim().is_empty() {
        status.unavailable_reasons.push(UnavailableReason {
            code: UnavailableReasonCode::MissingCredentials,
            detail: format!("llm provider {:?} requires credentials", provider_cfg.id),
        });
        return finalize(status);
    }

    let adapter_cfg = match resolve_runtime_provider_adapter(cfg, &provider_cfg, store) {
        Ok(value) => value,
        Err(err) => {
            status
                .unavailable_reasons
                .push(classify_resolution_error(&err));
            return finalize(status);
        }
    };
    status.auth_valid = true;

    let host_cfg = RuntimeProviderHostConfig {
        default: provider_cfg.id.clone(),
        providers: vec![adapter_cfg],
    };
    let host = match Host::new(&host_cfg) {
        Ok(host) => host,
        Err(err) => {
            status.unavailable_reasons.push(UnavailableReason {
                code: UnavailableReasonCode::UnsupportedRuntime,
                detail: err.to_string(),
            });
            return finalize(status);
        }
    };
    let provider = host.default_provider();

    match tokio::time::timeout(READINESS_PROBE_TIMEOUT, provider.ready()).await {
        Ok(Ok(())) => {
            status.ready = true;
            status.available = true;
        }
        Ok(Err(err)) => {
            status.unavailable_reasons.push(UnavailableReason {
                code: UnavailableReasonCode::NotReady,
                detail: err.to_string(),
            });
        }
        Err(_) => {
            status.unavailable_reasons.push(UnavailableReason {
                code: UnavailableReasonCode::NotReady,
                detail: "readiness check timed out".into(),
            });
        }
    }
    finalize(status)
}

fn finalize(mut status: CandidateStatus) -> CandidateStatus {
    status.available = status.configured
        && status.auth_valid
        && status.ready
        && status.unavailable_reasons.is_empty();
    status
}

fn classify_resolution_error(err: &rillan_config::ResolveError) -> UnavailableReason {
    if let rillan_config::ResolveError::Secret(inner) = err {
        if inner.is_not_found() {
            return UnavailableReason {
                code: UnavailableReasonCode::MissingCredentials,
                detail: err.to_string(),
            };
        }
    }
    UnavailableReason {
        code: UnavailableReasonCode::InvalidCredentials,
        detail: err.to_string(),
    }
}

fn requires_credential(provider: &ResolvedLlmProvider) -> bool {
    let strategy = provider.auth_strategy.trim().to_lowercase();
    !strategy.is_empty() && strategy != AUTH_STRATEGY_NONE
}
