// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! Skill metrics persistence. Mirrors `internal/agent/skill_metrics.go`.

use std::path::PathBuf;
use std::time::Duration;

use serde::{Deserialize, Serialize};
use thiserror::Error;
use time::format_description::well_known::Rfc3339;
use time::OffsetDateTime;

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct SkillMetric {
    pub skill_id: String,
    pub invocation_count: u64,
    pub last_observed_at: String,
    pub last_latency_millis: u64,
    pub average_latency_ms: u64,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct SkillMetricsStore {
    #[serde(default)]
    pub skills: Vec<SkillMetric>,
}

#[derive(Debug, Error)]
pub enum SkillMetricsError {
    #[error("read skill metrics: {0}")]
    Read(#[source] std::io::Error),
    #[error("create skill metrics dir: {0}")]
    CreateDir(#[source] std::io::Error),
    #[error("write skill metrics: {0}")]
    Write(#[source] std::io::Error),
    #[error("parse skill metrics: {0}")]
    Parse(#[source] serde_json::Error),
    #[error("marshal skill metrics: {0}")]
    Marshal(#[source] serde_json::Error),
    #[error("clock failed to format timestamp: {0}")]
    Time(#[source] time::error::Format),
}

#[must_use]
pub fn default_skill_metrics_path() -> PathBuf {
    rillan_config::default_data_dir()
        .join("agent")
        .join("skill_metrics.json")
}

pub fn load_skill_metrics() -> Result<SkillMetricsStore, SkillMetricsError> {
    load_skill_metrics_at(&default_skill_metrics_path())
}

fn load_skill_metrics_at(path: &std::path::Path) -> Result<SkillMetricsStore, SkillMetricsError> {
    match std::fs::read(path) {
        Ok(data) => {
            let mut store: SkillMetricsStore =
                serde_json::from_slice(&data).map_err(SkillMetricsError::Parse)?;
            store.skills.sort_by(|a, b| a.skill_id.cmp(&b.skill_id));
            Ok(store)
        }
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => Ok(SkillMetricsStore::default()),
        Err(err) => Err(SkillMetricsError::Read(err)),
    }
}

pub fn save_skill_metrics(store: SkillMetricsStore) -> Result<(), SkillMetricsError> {
    save_skill_metrics_at(&default_skill_metrics_path(), store)
}

fn save_skill_metrics_at(
    path: &std::path::Path,
    mut store: SkillMetricsStore,
) -> Result<(), SkillMetricsError> {
    if let Some(parent) = path.parent() {
        if !parent.as_os_str().is_empty() {
            std::fs::create_dir_all(parent).map_err(SkillMetricsError::CreateDir)?;
        }
    }
    store.skills.sort_by(|a, b| a.skill_id.cmp(&b.skill_id));
    let data = serde_json::to_vec_pretty(&store).map_err(SkillMetricsError::Marshal)?;
    std::fs::write(path, data).map_err(SkillMetricsError::Write)
}

/// Updates the persisted runtime-state record for one skill invocation.
pub fn record_skill_latency(
    skill_id: &str,
    duration: Duration,
    observed_at: OffsetDateTime,
) -> Result<(), SkillMetricsError> {
    record_skill_latency_at(
        &default_skill_metrics_path(),
        skill_id,
        duration,
        observed_at,
    )
}

fn record_skill_latency_at(
    path: &std::path::Path,
    skill_id: &str,
    duration: Duration,
    observed_at: OffsetDateTime,
) -> Result<(), SkillMetricsError> {
    let mut store = load_skill_metrics_at(path)?;
    let observed = observed_at
        .format(&Rfc3339)
        .map_err(SkillMetricsError::Time)?;
    let latency_ms = u64::try_from(duration.as_millis()).unwrap_or(u64::MAX);
    if let Some(metric) = store.skills.iter_mut().find(|m| m.skill_id == skill_id) {
        metric.invocation_count += 1;
        metric.last_observed_at = observed;
        metric.last_latency_millis = latency_ms;
        metric.average_latency_ms = rolling_average(
            metric.average_latency_ms,
            metric.invocation_count,
            latency_ms,
        );
    } else {
        store.skills.push(SkillMetric {
            skill_id: skill_id.to_string(),
            invocation_count: 1,
            last_observed_at: observed,
            last_latency_millis: latency_ms,
            average_latency_ms: latency_ms,
        });
    }
    save_skill_metrics_at(path, store)
}

fn rolling_average(previous: u64, count: u64, latest: u64) -> u64 {
    if count <= 1 {
        return latest;
    }
    ((previous * (count - 1)) + latest) / count
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn record_creates_new_skill_entry() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("skill_metrics.json");
        record_skill_latency_at(
            &path,
            "read_files",
            Duration::from_millis(42),
            OffsetDateTime::now_utc(),
        )
        .unwrap();
        let store = load_skill_metrics_at(&path).unwrap();
        assert_eq!(store.skills.len(), 1);
        assert_eq!(store.skills[0].skill_id, "read_files");
        assert_eq!(store.skills[0].invocation_count, 1);
        assert_eq!(store.skills[0].last_latency_millis, 42);
    }

    #[test]
    fn record_updates_existing_average() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("skill_metrics.json");
        for ms in [10u64, 30, 50] {
            record_skill_latency_at(
                &path,
                "search_repo",
                Duration::from_millis(ms),
                OffsetDateTime::now_utc(),
            )
            .unwrap();
        }
        let store = load_skill_metrics_at(&path).unwrap();
        assert_eq!(store.skills.len(), 1);
        assert_eq!(store.skills[0].invocation_count, 3);
        assert!(store.skills[0].average_latency_ms <= 50);
    }
}
