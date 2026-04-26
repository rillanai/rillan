// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! Append-only JSONL audit ledger. Mirrors `internal/audit` from the upstream
//! Go repo. ADR-010.

use std::path::{Path, PathBuf};

use async_trait::async_trait;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use thiserror::Error;
use time::format_description::well_known::Rfc3339;
use time::OffsetDateTime;
use tokio::io::AsyncWriteExt;
use tokio::sync::Mutex;

/// Event-type wire constants.
pub const EVENT_TYPE_REMOTE_EGRESS: &str = "remote_egress";
pub const EVENT_TYPE_REMOTE_DENY: &str = "remote_deny";
pub const EVENT_TYPE_AGENT_PROPOSAL: &str = "agent_action_proposed";
pub const EVENT_TYPE_AGENT_APPROVED: &str = "agent_action_approved";
pub const EVENT_TYPE_AGENT_DENIED: &str = "agent_action_denied";

/// One audit ledger entry. Field order matches the Go encoder so the JSONL is
/// byte-comparable across implementations.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct Event {
    /// Wall-clock timestamp at record time. Filled in automatically by
    /// [`Store::record`] when zero/empty.
    #[serde(default)]
    pub timestamp: String,
    #[serde(rename = "type", default)]
    pub kind: String,
    #[serde(default)]
    pub request_id: String,
    #[serde(default)]
    pub provider: String,
    #[serde(default)]
    pub model: String,
    #[serde(default)]
    pub verdict: String,
    #[serde(default)]
    pub reason: String,
    #[serde(default)]
    pub route_source: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub outbound_sha256: String,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub source_refs: Vec<String>,
    #[serde(default, skip_serializing_if = "is_zero_i64")]
    pub response_status: i64,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub response_sha256: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub error: String,
}

/// Async pluggable recorder. The HTTP layer holds an `Arc<dyn Recorder>` and
/// records every outbound request after policy evaluation.
#[async_trait]
pub trait Recorder: Send + Sync {
    async fn record(&self, event: Event) -> Result<(), Error>;
}

/// Errors raised by the file-backed [`Store`].
#[derive(Debug, Error)]
pub enum Error {
    #[error("create audit directory: {0}")]
    CreateDir(#[source] std::io::Error),
    #[error("open audit ledger: {0}")]
    Open(#[source] std::io::Error),
    #[error("append audit event: {0}")]
    Append(#[source] std::io::Error),
    #[error("read audit ledger: {0}")]
    Read(#[source] std::io::Error),
    #[error("marshal audit event: {0}")]
    Marshal(#[source] serde_json::Error),
    #[error("decode audit event: {0}")]
    Decode(#[source] serde_json::Error),
    #[error("clock failed to format timestamp: {0}")]
    Time(#[source] time::error::Format),
}

/// File-backed JSONL audit store. Cheap to clone — internally an `Arc`.
#[derive(Clone)]
pub struct Store {
    path: PathBuf,
    write_lock: std::sync::Arc<Mutex<()>>,
}

impl std::fmt::Debug for Store {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("Store").field("path", &self.path).finish()
    }
}

impl Store {
    /// Returns the default ledger path: `<data_dir>/audit/ledger.jsonl`.
    #[must_use]
    pub fn default_path() -> PathBuf {
        rillan_config::default_data_dir()
            .join("audit")
            .join("ledger.jsonl")
    }

    /// Creates the ledger's parent directory and returns a handle.
    pub fn new(path: impl Into<PathBuf>) -> Result<Self, Error> {
        let path = path.into();
        if let Some(parent) = path.parent() {
            if !parent.as_os_str().is_empty() {
                std::fs::create_dir_all(parent).map_err(Error::CreateDir)?;
            }
        }
        Ok(Self {
            path,
            write_lock: std::sync::Arc::new(Mutex::new(())),
        })
    }

    /// Returns the ledger path.
    #[must_use]
    pub fn path(&self) -> &Path {
        &self.path
    }

    /// Reads the entire ledger from disk. Missing file yields an empty vec.
    pub async fn read_all(&self) -> Result<Vec<Event>, Error> {
        let bytes = match tokio::fs::read(&self.path).await {
            Ok(bytes) => bytes,
            Err(err) if err.kind() == std::io::ErrorKind::NotFound => return Ok(Vec::new()),
            Err(err) => return Err(Error::Read(err)),
        };
        let mut events = Vec::new();
        for line in bytes.split(|b| *b == b'\n') {
            if line.iter().all(u8::is_ascii_whitespace) {
                continue;
            }
            let event: Event = serde_json::from_slice(line).map_err(Error::Decode)?;
            events.push(event);
        }
        Ok(events)
    }
}

#[async_trait]
impl Recorder for Store {
    async fn record(&self, mut event: Event) -> Result<(), Error> {
        if event.timestamp.is_empty() {
            event.timestamp = OffsetDateTime::now_utc()
                .format(&Rfc3339)
                .map_err(Error::Time)?;
        }
        let mut payload = serde_json::to_vec(&event).map_err(Error::Marshal)?;
        payload.push(b'\n');
        // Single-writer lock; the file handle is short-lived per record.
        let _guard = self.write_lock.lock().await;
        let mut file = tokio::fs::OpenOptions::new()
            .create(true)
            .append(true)
            .open(&self.path)
            .await
            .map_err(Error::Open)?;
        file.write_all(&payload).await.map_err(Error::Append)?;
        file.flush().await.map_err(Error::Append)?;
        Ok(())
    }
}

/// Returns the lowercase hex SHA-256 of `value`. Returns the empty string for
/// empty inputs to match the Go helper.
#[must_use]
pub fn hash_bytes(value: &[u8]) -> String {
    if value.is_empty() {
        return String::new();
    }
    let mut hasher = Sha256::new();
    hasher.update(value);
    let digest = hasher.finalize();
    hex_lower(&digest)
}

fn hex_lower(bytes: &[u8]) -> String {
    const HEX: &[u8; 16] = b"0123456789abcdef";
    let mut out = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        out.push(HEX[usize::from(byte >> 4)] as char);
        out.push(HEX[usize::from(byte & 0x0f)] as char);
    }
    out
}

#[allow(clippy::trivially_copy_pass_by_ref)]
fn is_zero_i64(value: &i64) -> bool {
    *value == 0
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn store_record_and_read_all() {
        let dir = tempfile::tempdir().expect("tempdir");
        let path = dir.path().join("audit").join("ledger.jsonl");
        let store = Store::new(path.clone()).expect("store");

        let event = Event {
            kind: EVENT_TYPE_REMOTE_EGRESS.into(),
            request_id: "req-1".into(),
            provider: "openai".into(),
            model: "gpt-4o-mini".into(),
            verdict: "allow".into(),
            reason: "policy_allow".into(),
            route_source: "default".into(),
            outbound_sha256: hash_bytes(b"payload"),
            source_refs: vec!["docs/guide.md:1-2".into()],
            response_status: 200,
            ..Event::default()
        };
        store.record(event).await.expect("record");

        let events = store.read_all().await.expect("read");
        assert_eq!(events.len(), 1);
        assert_eq!(events[0].request_id, "req-1");
        assert_eq!(events[0].outbound_sha256, hash_bytes(b"payload"));
        assert!(!events[0].timestamp.is_empty(), "timestamp filled in");
    }

    #[test]
    fn hash_bytes_returns_empty_for_empty_input() {
        assert_eq!(hash_bytes(&[]), "");
    }

    #[test]
    fn hash_bytes_matches_known_vector() {
        // SHA-256("payload") = 239f59ed55e737c77147cf55ad0c1b030b6d7ee748a7426952f9b852d5a935e5
        assert_eq!(
            hash_bytes(b"payload"),
            "239f59ed55e737c77147cf55ad0c1b030b6d7ee748a7426952f9b852d5a935e5",
        );
    }

    #[tokio::test]
    async fn read_all_returns_empty_when_file_missing() {
        let dir = tempfile::tempdir().expect("tempdir");
        let path = dir.path().join("missing.jsonl");
        let store = Store::new(path).expect("store");
        let events = store.read_all().await.expect("read");
        assert!(events.is_empty());
    }
}
