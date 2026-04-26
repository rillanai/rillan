// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! Read-only skill primitives. Mirrors `internal/agent/skills/read_only.go`.

use std::path::{Path, PathBuf};
use std::sync::{Arc, RwLock};

use thiserror::Error;
use tokio::process::Command;
use walkdir::WalkDir;

use crate::context_budget::trim_text;

use super::types::RepoMatch;

#[derive(Debug, Error)]
pub enum ResolveError {
    #[error("repo_root must not be empty")]
    Empty,
    #[error("repo root is not approved: {0}")]
    Unapproved(String),
    #[error("canonicalize path {path}: {source}")]
    Canonicalize {
        path: PathBuf,
        #[source]
        source: std::io::Error,
    },
}

impl ResolveError {
    /// True for the `unapproved repo root` sentinel — the HTTP layer maps
    /// this to a 400 instead of a 500.
    #[must_use]
    pub fn is_unapproved(&self) -> bool {
        matches!(self, Self::Unapproved(_))
    }
}

/// Resolves `repo_root` against the approved-roots allowlist. Returns the
/// canonical path on success.
pub fn resolve_approved_repo_root(
    repo_root: &str,
    approved_roots: &[String],
) -> Result<PathBuf, ResolveError> {
    if repo_root.trim().is_empty() {
        return Err(ResolveError::Empty);
    }
    let resolved = canonical_path(repo_root)?;
    if approved_roots.is_empty() {
        return Err(ResolveError::Unapproved(repo_root.to_string()));
    }
    for approved in approved_roots {
        let Ok(approved_canonical) = canonical_path(approved) else {
            continue;
        };
        if approved_canonical == resolved {
            return Ok(resolved);
        }
    }
    Err(ResolveError::Unapproved(repo_root.to_string()))
}

#[derive(Debug, Error)]
pub enum FsError {
    #[error("read file {path}: {source}")]
    ReadFile {
        path: PathBuf,
        #[source]
        source: std::io::Error,
    },
    #[error("walk repo {path}: {source}")]
    Walk {
        path: PathBuf,
        #[source]
        source: walkdir::Error,
    },
    #[error("path {path:?} escapes repo root")]
    PathEscape { path: String },
    #[error("canonicalize path {path}: {source}")]
    Canonicalize {
        path: PathBuf,
        #[source]
        source: std::io::Error,
    },
}

pub(crate) async fn read_file_bounded(
    repo_root: &Path,
    relative_path: &str,
    max_chars: usize,
) -> Result<String, FsError> {
    let resolved_root = canonical_pathbuf(repo_root)?;
    let resolved_path = resolve_repo_path(&resolved_root, relative_path)?;
    let data = tokio::fs::read(&resolved_path)
        .await
        .map_err(|err| FsError::ReadFile {
            path: resolved_path,
            source: err,
        })?;
    let text = String::from_utf8_lossy(&data).into_owned();
    Ok(trim_text(text, max_chars))
}

pub(crate) async fn search_repo_bounded(
    repo_root: &Path,
    query: &str,
    max_matches: usize,
    max_snippet_chars: usize,
) -> Result<Vec<RepoMatch>, FsError> {
    let resolved_root = canonical_pathbuf(repo_root)?;
    let needle = query.trim().to_lowercase();
    let mut results: Vec<RepoMatch> = Vec::with_capacity(max_matches);

    for entry in WalkDir::new(&resolved_root).follow_links(false) {
        let entry = entry.map_err(|err| FsError::Walk {
            path: resolved_root.clone(),
            source: err,
        })?;
        let file_type = entry.file_type();
        if file_type.is_symlink() {
            // Honor the same "in-repo symlinks pass; out-of-repo symlinks
            // skip" semantics as the Go version: re-canonicalize and check
            // containment.
            let Ok(resolved) = canonical_pathbuf(entry.path()) else {
                continue;
            };
            if !resolved.starts_with(&resolved_root) {
                continue;
            }
            // Read the resolved target as a regular file below.
        }
        if file_type.is_dir() {
            let name = entry.file_name().to_string_lossy().to_string();
            if matches!(name.as_str(), ".git" | "node_modules" | ".direnv" | ".idea") {
                // walkdir doesn't have an in-loop skip; relative-path
                // filtering at append time keeps these out anyway.
                continue;
            }
            continue;
        }
        let path = entry.path();
        let data = match tokio::fs::read(path).await {
            Ok(d) => d,
            Err(_) => continue,
        };
        let content = String::from_utf8_lossy(&data).into_owned();
        let lowered = content.to_lowercase();
        let Some(idx) = lowered.find(&needle) else {
            continue;
        };
        let rel = path.strip_prefix(&resolved_root).unwrap_or(path);
        let rel_path = rel.to_string_lossy().to_string();
        // Skip files inside ignored dirs that walkdir already descended into.
        if rel_path
            .split(std::path::MAIN_SEPARATOR)
            .any(|seg| matches!(seg, ".git" | "node_modules" | ".direnv" | ".idea"))
        {
            continue;
        }
        results.push(RepoMatch {
            path: rel_path,
            snippet: snippet_around(&content, idx, max_snippet_chars),
        });
        if results.len() >= max_matches {
            break;
        }
    }
    Ok(results)
}

fn snippet_around(content: &str, idx: usize, max_chars: usize) -> String {
    if max_chars == 0 || content.len() <= max_chars {
        return content.trim().to_string();
    }
    let half = max_chars / 2;
    let mut start = idx.saturating_sub(half);
    let mut end = start + max_chars;
    if end > content.len() {
        end = content.len();
        start = end.saturating_sub(max_chars);
    }
    // Snap to char boundaries to avoid splitting multi-byte UTF-8.
    while start > 0 && !content.is_char_boundary(start) {
        start -= 1;
    }
    while end < content.len() && !content.is_char_boundary(end) {
        end += 1;
    }
    trim_text(content[start..end].to_string(), max_chars)
}

fn canonical_pathbuf(path: &Path) -> Result<PathBuf, FsError> {
    path.canonicalize().map_err(|err| FsError::Canonicalize {
        path: path.to_path_buf(),
        source: err,
    })
}

fn canonical_path(path: &str) -> Result<PathBuf, ResolveError> {
    Path::new(path)
        .canonicalize()
        .map_err(|err| ResolveError::Canonicalize {
            path: PathBuf::from(path),
            source: err,
        })
}

fn resolve_repo_path(resolved_root: &Path, relative: &str) -> Result<PathBuf, FsError> {
    let combined = resolved_root.join(relative);
    let resolved = canonical_pathbuf(&combined)?;
    if !resolved.starts_with(resolved_root) {
        return Err(FsError::PathEscape {
            path: relative.to_string(),
        });
    }
    Ok(resolved)
}

/// Function pointer signature accepted by the git test seam.
pub(crate) type GitCommandFn =
    Arc<dyn Fn(PathBuf, Vec<String>) -> GitFuture + Send + Sync + 'static>;

pub(crate) type GitFuture = std::pin::Pin<
    Box<dyn std::future::Future<Output = Result<Vec<u8>, std::io::Error>> + Send + 'static>,
>;

static GIT_COMMAND: std::sync::OnceLock<RwLock<Option<GitCommandFn>>> = std::sync::OnceLock::new();

fn git_command_handle() -> &'static RwLock<Option<GitCommandFn>> {
    GIT_COMMAND.get_or_init(|| RwLock::new(None))
}

#[cfg(test)]
pub(crate) fn set_git_command_for_test(handler: GitCommandFn) -> Option<GitCommandFn> {
    let mut guard = git_command_handle().write().expect("git override poisoned");
    let previous = guard.take();
    *guard = Some(handler);
    previous
}

pub(crate) async fn run_git(repo_root: &Path, args: &[&str]) -> Result<String, GitError> {
    let owned_args: Vec<String> = args.iter().map(|s| (*s).to_string()).collect();
    let override_handler = git_command_handle()
        .read()
        .expect("git override poisoned")
        .clone();
    let output = if let Some(handler) = override_handler {
        handler(repo_root.to_path_buf(), owned_args.clone())
            .await
            .map_err(|err| GitError::Run {
                args: owned_args.join(" "),
                source: err,
                stderr: String::new(),
            })?
    } else {
        let mut command = Command::new("git");
        command.arg("-C").arg(repo_root);
        for arg in &owned_args {
            command.arg(arg);
        }
        let result = command.output().await.map_err(|err| GitError::Run {
            args: owned_args.join(" "),
            source: err,
            stderr: String::new(),
        })?;
        if !result.status.success() {
            return Err(GitError::Run {
                args: owned_args.join(" "),
                source: std::io::Error::other(result.status.to_string()),
                stderr: String::from_utf8_lossy(&result.stderr).trim().to_string(),
            });
        }
        result.stdout
    };
    let text = String::from_utf8_lossy(&output).trim().to_string();
    Ok(text)
}

#[derive(Debug, Error)]
pub enum GitError {
    #[error("git {args}: {source}: {stderr}")]
    Run {
        args: String,
        #[source]
        source: std::io::Error,
        stderr: String,
    },
}

pub(crate) fn split_non_empty_lines(value: &str) -> Vec<String> {
    value
        .trim()
        .split('\n')
        .map(str::trim)
        .filter(|s| !s.is_empty())
        .map(str::to_string)
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn split_non_empty_lines_drops_blank_lines() {
        let input = " M a\n\n?? b\n";
        assert_eq!(split_non_empty_lines(input), vec!["M a", "?? b"]);
    }

    #[test]
    fn snippet_around_returns_centered_window() {
        let content = "abcdefghijklmno";
        let snippet = snippet_around(content, 7, 6);
        assert!(snippet.contains('h'));
        assert!(snippet.len() <= 6);
    }
}
