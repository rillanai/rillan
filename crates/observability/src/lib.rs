// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! Lightweight observability primitives: request-id generation/propagation and
//! a counter+latency registry suitable for `/metrics` exposition.
//!
//! Mirrors `internal/observability` from the upstream Go repo.

use std::collections::BTreeMap;
use std::sync::Mutex;

use rand::RngCore;

/// HTTP header used to propagate a request id through the daemon.
pub const REQUEST_ID_HEADER: &str = "X-Request-ID";

/// Generates a new short hex request id.
///
/// Returns `"unknown"` if the system RNG fails — matches the Go implementation
/// rather than panicking, since the request id is observability-only.
#[must_use]
pub fn new_request_id() -> String {
    let mut buffer = [0_u8; 8];
    if rand::rngs::OsRng.try_fill_bytes(&mut buffer).is_err() {
        return "unknown".to_string();
    }
    hex_lower(&buffer)
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

/// In-process metrics registry. Counts HTTP requests by `(method, path,
/// status)` and tracks total handler duration in milliseconds per bucket.
#[derive(Debug, Default)]
pub struct Registry {
    inner: Mutex<RegistryInner>,
}

#[derive(Debug, Default)]
struct RegistryInner {
    requests: BTreeMap<(String, String, u16), CounterPair>,
}

#[derive(Debug, Default, Clone, Copy)]
struct CounterPair {
    count: u64,
    duration_ms: u64,
}

impl Registry {
    /// Creates an empty registry.
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }

    /// Records a single HTTP request.
    pub fn record_http_request(&self, method: &str, path: &str, status: u16, duration_ms: u64) {
        let mut inner = match self.inner.lock() {
            Ok(guard) => guard,
            Err(poison) => poison.into_inner(),
        };
        let entry = inner
            .requests
            .entry((method.to_owned(), path.to_owned(), status))
            .or_default();
        entry.count = entry.count.saturating_add(1);
        entry.duration_ms = entry.duration_ms.saturating_add(duration_ms);
    }

    /// Returns a snapshot of the recorded counters in a stable, sortable order.
    #[must_use]
    pub fn snapshot(&self) -> Vec<RequestSnapshot> {
        let inner = match self.inner.lock() {
            Ok(guard) => guard,
            Err(poison) => poison.into_inner(),
        };
        inner
            .requests
            .iter()
            .map(|((method, path, status), pair)| RequestSnapshot {
                method: method.clone(),
                path: path.clone(),
                status: *status,
                count: pair.count,
                duration_ms: pair.duration_ms,
            })
            .collect()
    }
}

/// One row of metrics output.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RequestSnapshot {
    pub method: String,
    pub path: String,
    pub status: u16,
    pub count: u64,
    pub duration_ms: u64,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn request_id_is_16_hex_chars() {
        let id = new_request_id();
        assert_eq!(id.len(), 16);
        assert!(id.chars().all(|c| c.is_ascii_hexdigit()));
    }

    #[test]
    fn registry_records_and_aggregates() {
        let registry = Registry::new();
        registry.record_http_request("GET", "/healthz", 200, 5);
        registry.record_http_request("GET", "/healthz", 200, 7);
        registry.record_http_request("POST", "/v1/chat/completions", 502, 12);

        let snapshot = registry.snapshot();
        assert_eq!(snapshot.len(), 2);

        let healthz = snapshot
            .iter()
            .find(|row| row.path == "/healthz")
            .expect("healthz row present");
        assert_eq!(healthz.count, 2);
        assert_eq!(healthz.duration_ms, 12);

        let chat = snapshot
            .iter()
            .find(|row| row.path == "/v1/chat/completions")
            .expect("chat row present");
        assert_eq!(chat.count, 1);
        assert_eq!(chat.status, 502);
    }
}
