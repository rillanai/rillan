// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! Hot-swappable runtime state. Mirrors `internal/app/runtime_manager.go`.
//!
//! The daemon publishes its current `RuntimeSnapshot` through an
//! [`arc_swap::ArcSwap`]. Refreshing acquires the build mutex (so concurrent
//! refreshes serialize), produces a new snapshot, and atomically swaps it in.

use std::future::Future;
use std::sync::Arc;

use arc_swap::ArcSwap;
use rillan_httpapi::RuntimeSnapshot;
use thiserror::Error;
use tokio::sync::Mutex;
use tracing::info;

/// Boxed future returned by [`Builder`].
type BuilderFuture =
    std::pin::Pin<Box<dyn Future<Output = Result<RuntimeSnapshot, BuildError>> + Send + 'static>>;

/// Builder closure for fresh snapshots.
pub type Builder = Arc<dyn Fn() -> BuilderFuture + Send + Sync + 'static>;

#[derive(Debug, Error)]
#[error("runtime snapshot build failed: {0}")]
pub struct BuildError(pub String);

/// Wraps an `ArcSwap<RuntimeSnapshot>` plus a build callback.
pub struct RuntimeManager {
    current: Arc<ArcSwap<RuntimeSnapshot>>,
    builder: Builder,
    refresh_lock: Mutex<()>,
}

impl std::fmt::Debug for RuntimeManager {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("RuntimeManager").finish_non_exhaustive()
    }
}

impl RuntimeManager {
    #[must_use]
    pub fn new(initial: RuntimeSnapshot, builder: Builder) -> Self {
        Self {
            current: Arc::new(ArcSwap::from_pointee(initial)),
            builder,
            refresh_lock: Mutex::new(()),
        }
    }

    /// Returns a clone of the latest snapshot.
    #[must_use]
    pub fn current_snapshot(&self) -> RuntimeSnapshot {
        RuntimeSnapshot::clone(&self.current.load_full())
    }

    /// Returns a closure suitable for `RouterOptions::runtime_snapshot`.
    #[must_use]
    pub fn snapshot_fn(self: &Arc<Self>) -> rillan_httpapi::RuntimeSnapshotFn {
        let manager = Arc::clone(self);
        Arc::new(move || manager.current_snapshot())
    }

    /// Returns a refresh callback suitable for
    /// `RouterOptions::refresh`. Errors propagate as Strings to the HTTP
    /// layer.
    #[must_use]
    pub fn refresh_fn(self: &Arc<Self>) -> rillan_httpapi::RefreshFn {
        let manager = Arc::clone(self);
        Arc::new(move || {
            let manager = Arc::clone(&manager);
            Box::pin(async move { manager.refresh().await.map_err(|err| err.to_string()) })
        })
    }

    /// Builds a fresh snapshot and swaps it in atomically.
    pub async fn refresh(&self) -> Result<(), BuildError> {
        let _guard = self.refresh_lock.lock().await;
        let next = (self.builder)().await?;
        let provider_name = next.provider.name().to_string();
        self.current.store(Arc::new(next));
        info!(provider = %provider_name, "runtime snapshot refreshed");
        Ok(())
    }
}

// `RuntimeManager` is a thin wrapper around `arc_swap::ArcSwap`. The
// interesting behavior — building a fresh `RuntimeSnapshot` from disk —
// lives in `runtime_snapshot_builder` and is exercised there.
