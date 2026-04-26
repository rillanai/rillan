// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! OS keyring abstraction with a swappable backend for tests.
//!
//! Mirrors `internal/secretstore` from the upstream Go repo. Production code
//! uses the real OS keyring; tests inject an in-memory backend.
//!
//! Unlike the Go original — which mutates package-level globals to override the
//! keyring — this crate exposes a [`Store`] value that owns its [`Backend`].
//! Callers thread the store through their constructors, which keeps tests
//! deterministic without needing process-wide locks.

use std::collections::HashMap;
use std::sync::{Arc, Mutex};

use serde::{Deserialize, Serialize};
use thiserror::Error;
use time::format_description::well_known::Rfc3339;
use time::OffsetDateTime;

/// Errors surfaced by store operations.
#[derive(Debug, Error)]
pub enum Error {
    #[error("credential not found")]
    NotFound,
    #[error("unsupported credential ref {0:?}")]
    UnsupportedRef(String),
    #[error("invalid keyring ref {0:?}")]
    InvalidRef(String),
    #[error("marshal credential: {0}")]
    Marshal(#[source] serde_json::Error),
    #[error("decode keyring credential: {0}")]
    Decode(#[source] serde_json::Error),
    #[error("read keyring credential: {0}")]
    Read(#[source] keyring::Error),
    #[error("write keyring credential: {0}")]
    Write(#[source] keyring::Error),
    #[error("delete keyring credential: {0}")]
    Delete(#[source] keyring::Error),
    #[error("stored credential endpoint {actual:?} does not match {expected:?}")]
    EndpointMismatch { actual: String, expected: String },
    #[error("stored credential auth strategy {actual:?} does not match {expected:?}")]
    AuthStrategyMismatch { actual: String, expected: String },
    #[error("stored credential issuer {actual:?} does not match {expected:?}")]
    IssuerMismatch { actual: String, expected: String },
    #[error("stored credential audience {actual:?} does not match {expected:?}")]
    AudienceMismatch { actual: String, expected: String },
    #[error("credential at {0} does not contain a bearer token or api key")]
    MissingBearer(String),
    #[error("credential at {0} does not contain an api key")]
    MissingApiKey(String),
    #[error("clock failed to format stored_at timestamp: {0}")]
    Time(#[source] time::error::Format),
}

impl Error {
    /// Helper used by callers that need to branch on "not found".
    #[must_use]
    pub fn is_not_found(&self) -> bool {
        matches!(self, Self::NotFound)
    }
}

/// Persisted secret payload. Mirrors the Go `Credential` struct field-for-field
/// so JSON encoded by either implementation round-trips through the other.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct Credential {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub kind: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub api_key: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub access_token: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub refresh_token: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub id_token: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub endpoint: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub auth_strategy: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub issuer: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub audience: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub stored_at: String,
}

/// Endpoint binding constraints applied when reusing a stored credential.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct Binding {
    pub endpoint: String,
    pub auth_strategy: String,
    pub issuer: String,
    pub audience: String,
}

/// Pluggable keyring backend.
pub trait Backend: Send + Sync {
    fn get(&self, service: &str, account: &str) -> Result<String, Error>;
    fn set(&self, service: &str, account: &str, password: &str) -> Result<(), Error>;
    fn delete(&self, service: &str, account: &str) -> Result<(), Error>;
}

/// Keyring-backed store. Cheap to clone — the backend lives behind an [`Arc`].
#[derive(Clone)]
pub struct Store {
    backend: Arc<dyn Backend>,
}

impl std::fmt::Debug for Store {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("Store").finish_non_exhaustive()
    }
}

impl Store {
    /// Creates a store backed by the OS keyring.
    #[must_use]
    pub fn os_keyring() -> Self {
        Self {
            backend: Arc::new(OsBackend),
        }
    }

    /// Creates a store backed by an in-memory map. Intended for tests.
    #[must_use]
    pub fn in_memory() -> Self {
        Self {
            backend: Arc::new(MemoryBackend::default()),
        }
    }

    /// Wraps an arbitrary [`Backend`].
    #[must_use]
    pub fn with_backend(backend: Arc<dyn Backend>) -> Self {
        Self { backend }
    }

    /// Saves a credential at the keyring reference identified by `keyring_ref`.
    pub fn save(&self, keyring_ref: &str, mut credential: Credential) -> Result<(), Error> {
        let (service, account) = parse_ref(keyring_ref)?;
        credential.stored_at = OffsetDateTime::now_utc()
            .format(&Rfc3339)
            .map_err(Error::Time)?;
        let payload = serde_json::to_string(&credential).map_err(Error::Marshal)?;
        self.backend.set(&service, &account, &payload)
    }

    /// Loads and decodes a credential from the keyring.
    pub fn load(&self, keyring_ref: &str) -> Result<Credential, Error> {
        let (service, account) = parse_ref(keyring_ref)?;
        let value = self.backend.get(&service, &account)?;
        serde_json::from_str(&value).map_err(Error::Decode)
    }

    /// Deletes the credential at `keyring_ref`. Idempotent on missing entries.
    pub fn delete(&self, keyring_ref: &str) -> Result<(), Error> {
        let (service, account) = parse_ref(keyring_ref)?;
        match self.backend.delete(&service, &account) {
            Ok(()) => Ok(()),
            Err(Error::NotFound) => Ok(()),
            Err(err) => Err(err),
        }
    }

    /// Reports whether a credential currently exists at `keyring_ref`.
    #[must_use]
    pub fn exists(&self, keyring_ref: &str) -> bool {
        self.load(keyring_ref).is_ok()
    }

    /// Returns the bearer token to use for a bound provider or MCP entry,
    /// rejecting credentials whose binding has drifted from `binding`.
    pub fn resolve_bearer(&self, keyring_ref: &str, binding: &Binding) -> Result<String, Error> {
        let credential = self.load(keyring_ref)?;
        validate_binding(&credential, binding)?;
        if !credential.access_token.is_empty() {
            return Ok(credential.access_token);
        }
        if !credential.api_key.is_empty() {
            return Ok(credential.api_key);
        }
        Err(Error::MissingBearer(keyring_ref.to_string()))
    }
}

impl Default for Store {
    fn default() -> Self {
        Self::os_keyring()
    }
}

fn validate_binding(credential: &Credential, binding: &Binding) -> Result<(), Error> {
    if !binding.endpoint.is_empty()
        && !credential.endpoint.is_empty()
        && credential.endpoint != binding.endpoint
    {
        return Err(Error::EndpointMismatch {
            actual: credential.endpoint.clone(),
            expected: binding.endpoint.clone(),
        });
    }
    if !binding.auth_strategy.is_empty()
        && !credential.auth_strategy.is_empty()
        && credential.auth_strategy != binding.auth_strategy
    {
        return Err(Error::AuthStrategyMismatch {
            actual: credential.auth_strategy.clone(),
            expected: binding.auth_strategy.clone(),
        });
    }
    if !binding.issuer.is_empty()
        && !credential.issuer.is_empty()
        && credential.issuer != binding.issuer
    {
        return Err(Error::IssuerMismatch {
            actual: credential.issuer.clone(),
            expected: binding.issuer.clone(),
        });
    }
    if !binding.audience.is_empty()
        && !credential.audience.is_empty()
        && credential.audience != binding.audience
    {
        return Err(Error::AudienceMismatch {
            actual: credential.audience.clone(),
            expected: binding.audience.clone(),
        });
    }
    Ok(())
}

/// Validates that the credential's stored binding fields agree with `binding`.
/// Exposed for the config crate, which performs the same check before handing
/// raw API keys to providers.
pub fn check_binding(credential: &Credential, binding: &Binding) -> Result<(), Error> {
    validate_binding(credential, binding)
}

fn parse_ref(value: &str) -> Result<(String, String), Error> {
    let trimmed = value.trim();
    let Some(rest) = trimmed.strip_prefix("keyring://") else {
        return Err(Error::UnsupportedRef(value.to_string()));
    };
    match rest.rfind('/') {
        Some(idx) if idx > 0 && idx < rest.len() - 1 => {
            Ok((rest[..idx].to_string(), rest[idx + 1..].to_string()))
        }
        _ => Err(Error::InvalidRef(value.to_string())),
    }
}

struct OsBackend;

impl Backend for OsBackend {
    fn get(&self, service: &str, account: &str) -> Result<String, Error> {
        let entry = keyring::Entry::new(service, account).map_err(Error::Read)?;
        match entry.get_password() {
            Ok(value) => Ok(value),
            Err(keyring::Error::NoEntry) => Err(Error::NotFound),
            Err(err) => Err(Error::Read(err)),
        }
    }

    fn set(&self, service: &str, account: &str, password: &str) -> Result<(), Error> {
        let entry = keyring::Entry::new(service, account).map_err(Error::Write)?;
        entry.set_password(password).map_err(Error::Write)
    }

    fn delete(&self, service: &str, account: &str) -> Result<(), Error> {
        let entry = keyring::Entry::new(service, account).map_err(Error::Delete)?;
        match entry.delete_credential() {
            Ok(()) => Ok(()),
            Err(keyring::Error::NoEntry) => Err(Error::NotFound),
            Err(err) => Err(Error::Delete(err)),
        }
    }
}

#[derive(Default)]
struct MemoryBackend {
    inner: Mutex<HashMap<String, String>>,
}

impl MemoryBackend {
    fn key(service: &str, account: &str) -> String {
        format!("{service}/{account}")
    }
}

impl Backend for MemoryBackend {
    fn get(&self, service: &str, account: &str) -> Result<String, Error> {
        let map = self.inner.lock().expect("memory backend mutex poisoned");
        map.get(&Self::key(service, account))
            .cloned()
            .ok_or(Error::NotFound)
    }

    fn set(&self, service: &str, account: &str, password: &str) -> Result<(), Error> {
        let mut map = self.inner.lock().expect("memory backend mutex poisoned");
        map.insert(Self::key(service, account), password.to_string());
        Ok(())
    }

    fn delete(&self, service: &str, account: &str) -> Result<(), Error> {
        let mut map = self.inner.lock().expect("memory backend mutex poisoned");
        match map.remove(&Self::key(service, account)) {
            Some(_) => Ok(()),
            None => Err(Error::NotFound),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn store() -> Store {
        Store::in_memory()
    }

    #[test]
    fn save_load_and_delete_credential() {
        let store = store();
        let keyring_ref = "keyring://rillan/llm/work-gpt";
        store
            .save(
                keyring_ref,
                Credential {
                    kind: "api_key".into(),
                    api_key: "secret".into(),
                    endpoint: "https://api.openai.com/v1".into(),
                    auth_strategy: "api_key".into(),
                    ..Credential::default()
                },
            )
            .expect("save");

        let credential = store.load(keyring_ref).expect("load");
        assert_eq!(credential.api_key, "secret");

        store.delete(keyring_ref).expect("delete");
        assert!(!store.exists(keyring_ref));
    }

    #[test]
    fn resolve_bearer_rejects_binding_mismatch() {
        let store = store();
        let keyring_ref = "keyring://rillan/llm/work-gpt";
        store
            .save(
                keyring_ref,
                Credential {
                    kind: "oidc".into(),
                    access_token: "token".into(),
                    endpoint: "https://api.openai.com/v1".into(),
                    auth_strategy: "browser_oidc".into(),
                    issuer: "issuer-a".into(),
                    ..Credential::default()
                },
            )
            .expect("save");

        let err = store
            .resolve_bearer(
                keyring_ref,
                &Binding {
                    endpoint: "https://api.openai.com/v1".into(),
                    auth_strategy: "browser_oidc".into(),
                    issuer: "issuer-b".into(),
                    ..Binding::default()
                },
            )
            .expect_err("issuer mismatch should fail");
        assert!(matches!(err, Error::IssuerMismatch { .. }));
    }

    #[test]
    fn load_returns_not_found() {
        let store = store();
        let err = store
            .load("keyring://rillan/auth/team-default")
            .expect_err("not found");
        assert!(err.is_not_found());
    }

    #[test]
    fn parse_ref_rejects_bad_inputs() {
        assert!(matches!(parse_ref("oops"), Err(Error::UnsupportedRef(_)),));
        assert!(matches!(
            parse_ref("keyring://broken"),
            Err(Error::InvalidRef(_)),
        ));
        let (service, account) = parse_ref("keyring://service/path/account").expect("ok");
        assert_eq!(service, "service/path");
        assert_eq!(account, "account");
    }
}
