// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! Read-only skill registry. Mirrors `internal/agent/skills/registry.go`.

use std::path::PathBuf;

use rillan_index::{default_db_path, Store as IndexStore, StoreError as IndexStoreError};
use thiserror::Error;

use crate::context_budget::trim_text;

use super::read_only::{
    read_file_bounded, resolve_approved_repo_root, run_git, search_repo_bounded,
    split_non_empty_lines, FsError, GitError, ResolveError,
};
use super::types::{
    FileContent, GitDiffRequest, GitDiffResult, GitStatusRequest, GitStatusResult,
    IndexLookupRequest, IndexLookupResult, IndexMatch, ReadFilesRequest, ReadFilesResult,
    SearchRepoRequest, SearchRepoResult,
};

const DEFAULT_MAX_FILES: usize = 8;
const DEFAULT_MAX_CHARS_PER_FILE: usize = 2000;
const DEFAULT_MAX_MATCHES: usize = 10;
const DEFAULT_MAX_SNIPPET_CHARS: usize = 240;
const DEFAULT_MAX_GIT_ENTRIES: usize = 20;
const DEFAULT_MAX_GIT_DIFF_CHARS: usize = 4000;

/// Errors raised by [`Registry`] methods.
#[derive(Debug, Error)]
pub enum SkillError {
    #[error("read_files.repo_root must not be empty")]
    ReadFilesRepoRootEmpty,
    #[error("read_files.paths must not be empty")]
    ReadFilesPathsEmpty,
    #[error("read_files.paths exceeds limit of {0}")]
    ReadFilesTooManyPaths(usize),
    #[error("read_files.paths contains invalid path")]
    ReadFilesBadPath,
    #[error("search_repo.repo_root must not be empty")]
    SearchRepoRootEmpty,
    #[error("search_repo.query must not be empty")]
    SearchRepoQueryEmpty,
    #[error("index_lookup.query must not be empty")]
    IndexLookupQueryEmpty,
    #[error("git_status.repo_root must not be empty")]
    GitStatusRepoRootEmpty,
    #[error("git_diff.repo_root must not be empty")]
    GitDiffRepoRootEmpty,
    #[error("resolve repo root: {0}")]
    Resolve(#[from] ResolveError),
    #[error("filesystem: {0}")]
    Fs(#[from] FsError),
    #[error("git: {0}")]
    Git(#[from] GitError),
    #[error("index store: {0}")]
    Index(#[from] IndexStoreError),
}

impl SkillError {
    /// True for unapproved-repo-root sentinels — surface as 400 in HTTP.
    #[must_use]
    pub fn is_unapproved(&self) -> bool {
        matches!(self, Self::Resolve(err) if err.is_unapproved())
    }
}

/// Read-only skill registry. Holds the approved-roots allowlist and provides
/// the `read_files` / `search_repo` / `index_lookup` / `git_status` /
/// `git_diff` skills.
#[derive(Debug, Clone)]
pub struct Registry {
    approved_repo_roots: Vec<String>,
}

impl Registry {
    /// Builds a registry restricted to `approved_repo_roots`.
    #[must_use]
    pub fn new(approved_repo_roots: Vec<String>) -> Self {
        Self {
            approved_repo_roots,
        }
    }

    /// Returns the configured approved roots.
    #[must_use]
    pub fn approved_repo_roots(&self) -> &[String] {
        &self.approved_repo_roots
    }

    pub async fn read_files(&self, req: ReadFilesRequest) -> Result<ReadFilesResult, SkillError> {
        let (paths, max_chars) = validate_read_files_request(&req)?;
        let approved_root = resolve_approved_repo_root(&req.repo_root, &self.approved_repo_roots)?;
        let mut files = Vec::with_capacity(paths.len());
        for path in paths {
            let content = read_file_bounded(&approved_root, &path, max_chars).await?;
            files.push(FileContent { path, content });
        }
        Ok(ReadFilesResult { files })
    }

    pub async fn search_repo(
        &self,
        req: SearchRepoRequest,
    ) -> Result<SearchRepoResult, SkillError> {
        let max_matches = positive_or_default(req.max_matches, DEFAULT_MAX_MATCHES);
        let max_snippet_chars =
            positive_or_default(req.max_snippet_chars, DEFAULT_MAX_SNIPPET_CHARS);
        if req.repo_root.trim().is_empty() {
            return Err(SkillError::SearchRepoRootEmpty);
        }
        if req.query.trim().is_empty() {
            return Err(SkillError::SearchRepoQueryEmpty);
        }
        let approved_root = resolve_approved_repo_root(&req.repo_root, &self.approved_repo_roots)?;
        let matches =
            search_repo_bounded(&approved_root, &req.query, max_matches, max_snippet_chars).await?;
        Ok(SearchRepoResult { matches })
    }

    pub async fn index_lookup(
        &self,
        req: IndexLookupRequest,
    ) -> Result<IndexLookupResult, SkillError> {
        let max_matches = positive_or_default(req.max_matches, DEFAULT_MAX_MATCHES);
        let max_snippet_chars =
            positive_or_default(req.max_snippet_chars, DEFAULT_MAX_SNIPPET_CHARS);
        if req.query.trim().is_empty() {
            return Err(SkillError::IndexLookupQueryEmpty);
        }
        let db_path = if req.db_path.trim().is_empty() {
            default_db_path()
        } else {
            PathBuf::from(req.db_path.clone())
        };
        let store = IndexStore::open(db_path)?;
        let results = store.search_chunks_keyword(&req.query, max_matches)?;
        let matches = results
            .into_iter()
            .map(|result| IndexMatch {
                path: result.document_path.clone(),
                ref_: format!(
                    "{}:{}-{}",
                    result.document_path, result.start_line, result.end_line
                ),
                snippet: trim_text(result.content, max_snippet_chars),
            })
            .collect();
        Ok(IndexLookupResult { matches })
    }

    pub async fn git_status(&self, req: GitStatusRequest) -> Result<GitStatusResult, SkillError> {
        if req.repo_root.trim().is_empty() {
            return Err(SkillError::GitStatusRepoRootEmpty);
        }
        let approved_root = resolve_approved_repo_root(&req.repo_root, &self.approved_repo_roots)?;
        let output = run_git(&approved_root, &["status", "--short"]).await?;
        let mut entries = split_non_empty_lines(&output);
        let limit = positive_or_default(req.max_entries, DEFAULT_MAX_GIT_ENTRIES);
        if entries.len() > limit {
            entries.truncate(limit);
        }
        Ok(GitStatusResult { entries })
    }

    pub async fn git_diff(&self, req: GitDiffRequest) -> Result<GitDiffResult, SkillError> {
        if req.repo_root.trim().is_empty() {
            return Err(SkillError::GitDiffRepoRootEmpty);
        }
        let approved_root = resolve_approved_repo_root(&req.repo_root, &self.approved_repo_roots)?;
        let mut args: Vec<&str> = vec!["diff", "--no-ext-diff"];
        if req.staged_only {
            args.push("--staged");
        }
        let output = run_git(&approved_root, &args).await?;
        let trimmed = trim_text(
            output,
            positive_or_default(req.max_chars, DEFAULT_MAX_GIT_DIFF_CHARS),
        );
        Ok(GitDiffResult { diff: trimmed })
    }
}

fn validate_read_files_request(req: &ReadFilesRequest) -> Result<(Vec<String>, usize), SkillError> {
    if req.repo_root.trim().is_empty() {
        return Err(SkillError::ReadFilesRepoRootEmpty);
    }
    if req.paths.is_empty() {
        return Err(SkillError::ReadFilesPathsEmpty);
    }
    let max_files = positive_or_default(req.max_files, DEFAULT_MAX_FILES);
    if req.paths.len() > max_files {
        return Err(SkillError::ReadFilesTooManyPaths(max_files));
    }
    let mut cleaned = Vec::with_capacity(req.paths.len());
    for raw in &req.paths {
        let path = clean_relative(raw);
        if path.is_empty() || path == "." {
            return Err(SkillError::ReadFilesBadPath);
        }
        cleaned.push(path);
    }
    Ok((
        cleaned,
        positive_or_default(req.max_chars_per_file, DEFAULT_MAX_CHARS_PER_FILE),
    ))
}

fn clean_relative(raw: &str) -> String {
    let trimmed = raw.trim();
    if trimmed.is_empty() {
        return String::new();
    }
    // Mirrors `filepath.Clean`: collapse `./`, `..`, repeated separators.
    let mut parts: Vec<&str> = Vec::new();
    for segment in trimmed.split(['/', '\\']) {
        match segment {
            "" | "." => {}
            ".." => {
                parts.pop();
            }
            other => parts.push(other),
        }
    }
    if parts.is_empty() {
        return String::new();
    }
    parts.join(std::path::MAIN_SEPARATOR_STR)
}

fn positive_or_default(value: usize, fallback: usize) -> usize {
    if value > 0 {
        value
    } else {
        fallback
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::Arc;

    fn write_fixture(repo: &std::path::Path, rel: &str, content: &str) {
        let path = repo.join(rel);
        std::fs::create_dir_all(path.parent().unwrap()).unwrap();
        std::fs::write(&path, content).unwrap();
    }

    #[tokio::test]
    async fn read_files_returns_bounded_content() {
        let tmp = tempfile::tempdir().unwrap();
        let repo = tmp.path().to_path_buf();
        write_fixture(&repo, "docs/guide.md", "hello world from file");
        let registry = Registry::new(vec![repo.to_string_lossy().to_string()]);
        let result = registry
            .read_files(ReadFilesRequest {
                repo_root: repo.to_string_lossy().to_string(),
                paths: vec!["docs/guide.md".into()],
                max_files: 2,
                max_chars_per_file: 5,
            })
            .await
            .expect("read");
        assert_eq!(result.files.len(), 1);
        assert_ne!(result.files[0].content, "hello world from file");
    }

    #[tokio::test]
    async fn read_files_rejects_symlink_escape() {
        if cfg!(target_os = "windows") {
            return;
        }
        let tmp = tempfile::tempdir().unwrap();
        let repo = tmp.path().to_path_buf();
        let outside_dir = tempfile::tempdir().unwrap();
        let outside_file = outside_dir.path().join("secret.txt");
        std::fs::write(&outside_file, b"outside").unwrap();
        let docs = repo.join("docs");
        std::fs::create_dir_all(&docs).unwrap();
        std::os::unix::fs::symlink(&outside_file, docs.join("escape.txt")).unwrap();
        let registry = Registry::new(vec![repo.to_string_lossy().to_string()]);
        let err = registry
            .read_files(ReadFilesRequest {
                repo_root: repo.to_string_lossy().to_string(),
                paths: vec!["docs/escape.txt".into()],
                max_files: 1,
                max_chars_per_file: 40,
            })
            .await
            .expect_err("symlink escape must fail");
        assert!(matches!(err, SkillError::Fs(FsError::PathEscape { .. })));
    }

    #[tokio::test]
    async fn search_repo_returns_bounded_matches() {
        let tmp = tempfile::tempdir().unwrap();
        let repo = tmp.path().to_path_buf();
        write_fixture(&repo, "docs/guide.md", "retrieval context is useful");
        let registry = Registry::new(vec![repo.to_string_lossy().to_string()]);
        let result = registry
            .search_repo(SearchRepoRequest {
                repo_root: repo.to_string_lossy().to_string(),
                query: "context".into(),
                max_matches: 3,
                max_snippet_chars: 40,
            })
            .await
            .expect("search");
        assert_eq!(result.matches.len(), 1);
    }

    #[tokio::test]
    async fn git_status_uses_test_seam() {
        let tmp = tempfile::tempdir().unwrap();
        let repo = tmp.path().to_path_buf();
        let previous =
            super::super::read_only::set_git_command_for_test(Arc::new(|_root, args| {
                Box::pin(async move {
                    if args.first().map(|s| s.as_str()) == Some("status") {
                        Ok(b" M a.go\n?? b.go\n".to_vec())
                    } else {
                        Ok(Vec::new())
                    }
                })
            }));
        let registry = Registry::new(vec![repo.to_string_lossy().to_string()]);
        let result = registry
            .git_status(GitStatusRequest {
                repo_root: repo.to_string_lossy().to_string(),
                max_entries: 5,
            })
            .await
            .expect("status");
        assert_eq!(result.entries.len(), 2);
        // restore previous handler if any
        if let Some(p) = previous {
            super::super::read_only::set_git_command_for_test(p);
        }
    }

    #[tokio::test]
    async fn git_status_rejects_unapproved_repo_root() {
        let tmp = tempfile::tempdir().unwrap();
        let repo = tmp.path().to_path_buf();
        let registry = Registry::new(Vec::new());
        let err = registry
            .git_status(GitStatusRequest {
                repo_root: repo.to_string_lossy().to_string(),
                max_entries: 5,
            })
            .await
            .expect_err("unapproved");
        assert!(err.is_unapproved());
    }
}
