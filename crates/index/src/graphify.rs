// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! Knowledge-graph (graphify) discovery + status. Mirrors
//! `internal/index/graphify.go` and `internal/index/graphify_status.go`.

use std::fs;
use std::io;
use std::path::{Path, PathBuf};

use rillan_config::KnowledgeGraphConfig;
use serde::Deserialize;
use sha2::{Digest, Sha256};
use thiserror::Error;
use walkdir::WalkDir;

use crate::types::SourceFile;

const GRAPHIFY_PREFIX: &str = "graphify/";

#[derive(Debug, Error)]
pub enum GraphifyError {
    #[error("resolve knowledge graph path: {0}")]
    Resolve(#[source] io::Error),
    #[error("stat knowledge graph path: {0}")]
    Stat(#[source] io::Error),
    #[error("walk knowledge graph files: {0}")]
    Walk(#[source] walkdir::Error),
    #[error("read graphify file {path}: {source}")]
    Read {
        path: PathBuf,
        #[source]
        source: io::Error,
    },
    #[error("parse graph.json: {0}")]
    ParseGraph(#[source] serde_json::Error),
}

/// Discovers graphify-emitted files for inclusion in the index.
pub fn discover_graphify_files(
    cfg: &KnowledgeGraphConfig,
) -> Result<Vec<SourceFile>, GraphifyError> {
    let path = cfg.path.trim();
    if !cfg.enabled || path.is_empty() {
        return Ok(Vec::new());
    }
    let root = match Path::new(path).canonicalize() {
        Ok(value) => value,
        Err(err) if err.kind() == io::ErrorKind::NotFound => return Ok(Vec::new()),
        Err(err) => return Err(GraphifyError::Resolve(err)),
    };
    if !fs::metadata(&root).map_err(GraphifyError::Stat)?.is_dir() {
        return Ok(Vec::new());
    }
    let mut files: Vec<SourceFile> = Vec::new();

    let graph_path = root.join("graph.json");
    if let Ok(graph_data) = fs::read(&graph_path) {
        let content = summarize_graph_json(&graph_data, cfg)?;
        let size = content.len() as u64;
        files.push(SourceFile {
            absolute_path: graph_path.clone(),
            relative_path: format!("{GRAPHIFY_PREFIX}graph.json"),
            content,
            size_bytes: size,
        });
    }

    for entry in WalkDir::new(&root).follow_links(false) {
        let entry = entry.map_err(GraphifyError::Walk)?;
        if entry.file_type().is_dir() {
            continue;
        }
        if entry.path().extension().and_then(|s| s.to_str()) != Some("md") {
            continue;
        }
        let rel = entry
            .path()
            .strip_prefix(&root)
            .unwrap_or(entry.path())
            .to_string_lossy()
            .replace('\\', "/");
        let data = fs::read(entry.path()).map_err(|err| GraphifyError::Read {
            path: entry.path().to_path_buf(),
            source: err,
        })?;
        let content = normalize_content(std::str::from_utf8(&data).unwrap_or(""));
        if content.trim().is_empty() {
            continue;
        }
        let size = data.len() as u64;
        files.push(SourceFile {
            absolute_path: entry.path().to_path_buf(),
            relative_path: format!("{GRAPHIFY_PREFIX}{rel}"),
            content,
            size_bytes: size,
        });
    }

    files.sort_by(|a, b| a.relative_path.cmp(&b.relative_path));
    Ok(files)
}

/// Snapshot of the graph at `cfg.path`.
#[derive(Debug, Clone, Default, PartialEq)]
pub struct GraphifyStatus {
    pub enabled: bool,
    pub path: String,
    pub present: bool,
    pub nodes: u64,
    pub edges: u64,
    pub sha256: String,
}

/// Reads the graphify status: whether `graph.json` exists, its node/edge
/// counts, and a hex SHA-256 checksum.
pub fn read_graphify_status(cfg: &KnowledgeGraphConfig) -> Result<GraphifyStatus, GraphifyError> {
    let mut status = GraphifyStatus {
        enabled: cfg.enabled,
        path: cfg.path.trim().to_string(),
        ..GraphifyStatus::default()
    };
    if !cfg.enabled || status.path.is_empty() {
        return Ok(status);
    }
    let abs = match Path::new(&status.path).canonicalize() {
        Ok(p) => p,
        Err(err) if err.kind() == io::ErrorKind::NotFound => {
            return Ok(status);
        }
        Err(err) => return Err(GraphifyError::Resolve(err)),
    };
    status.path = abs.to_string_lossy().to_string();
    let graph_path = abs.join("graph.json");
    let data = match fs::read(&graph_path) {
        Ok(d) => d,
        Err(err) if err.kind() == io::ErrorKind::NotFound => return Ok(status),
        Err(err) => {
            return Err(GraphifyError::Read {
                path: graph_path,
                source: err,
            })
        }
    };
    status.present = true;
    let mut hasher = Sha256::new();
    hasher.update(&data);
    let digest = hasher.finalize();
    status.sha256 = digest
        .iter()
        .map(|b| format!("{b:02x}"))
        .collect::<String>();

    let parsed: GraphCounts = serde_json::from_slice(&data).map_err(GraphifyError::ParseGraph)?;
    status.nodes = parsed.nodes.len() as u64;
    status.edges = parsed.edges.len() as u64;
    Ok(status)
}

#[derive(Debug, Default, Deserialize)]
struct GraphifyGraph {
    #[serde(default)]
    nodes: Vec<serde_json::Map<String, serde_json::Value>>,
    #[serde(default)]
    edges: Vec<serde_json::Value>,
}

#[derive(Debug, Default, Deserialize)]
struct GraphCounts {
    #[serde(default)]
    nodes: Vec<serde_json::Value>,
    #[serde(default)]
    edges: Vec<serde_json::Value>,
}

fn summarize_graph_json(data: &[u8], cfg: &KnowledgeGraphConfig) -> Result<String, GraphifyError> {
    let graph: GraphifyGraph = serde_json::from_slice(data).map_err(GraphifyError::ParseGraph)?;
    let limit = if cfg.max_nodes <= 0 {
        rillan_config::default_config().knowledge_graph.max_nodes
    } else {
        cfg.max_nodes
    };
    let limit = usize::try_from(limit).unwrap_or(0).min(graph.nodes.len());
    let mut lines: Vec<String> = vec![
        format!("nodes: {}", graph.nodes.len()),
        format!("edges: {}", graph.edges.len()),
    ];
    for (i, node) in graph.nodes.iter().enumerate().take(limit) {
        let id = node.get("id").map(format_value).unwrap_or_default();
        let label = node.get("label").map(format_value).unwrap_or_default();
        let kind = node.get("type").map(format_value).unwrap_or_default();
        lines.push(format!("node[{i}]: id={id} label={label} type={kind}"));
    }
    Ok(lines.join("\n"))
}

fn format_value(value: &serde_json::Value) -> String {
    match value {
        serde_json::Value::String(s) => s.clone(),
        other => other.to_string(),
    }
}

fn normalize_content(content: &str) -> String {
    content.replace("\r\n", "\n").replace('\r', "\n")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn read_status_returns_disabled_when_disabled() {
        let cfg = KnowledgeGraphConfig {
            enabled: false,
            path: String::new(),
            ..KnowledgeGraphConfig::default()
        };
        let status = read_graphify_status(&cfg).expect("status");
        assert!(!status.enabled);
        assert!(!status.present);
    }

    #[test]
    fn read_status_returns_absent_when_path_missing() {
        let dir = tempfile::tempdir().unwrap();
        let cfg = KnowledgeGraphConfig {
            enabled: true,
            path: dir.path().join("missing").to_string_lossy().to_string(),
            ..KnowledgeGraphConfig::default()
        };
        let status = read_graphify_status(&cfg).expect("status");
        assert!(status.enabled);
        assert!(!status.present);
    }

    #[test]
    fn read_status_counts_nodes_and_edges() {
        let dir = tempfile::tempdir().unwrap();
        let graph_path = dir.path().join("graph.json");
        std::fs::write(
            &graph_path,
            r#"{"nodes":[{"id":"a"},{"id":"b"}],"edges":[{"from":"a","to":"b"}]}"#,
        )
        .unwrap();
        let cfg = KnowledgeGraphConfig {
            enabled: true,
            path: dir.path().to_string_lossy().to_string(),
            max_nodes: 10,
            ..KnowledgeGraphConfig::default()
        };
        let status = read_graphify_status(&cfg).expect("status");
        assert!(status.present);
        assert_eq!(status.nodes, 2);
        assert_eq!(status.edges, 1);
    }
}
