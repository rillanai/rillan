// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! Read-only agent skills. Mirrors `internal/agent/skills/` from the upstream
//! Go repo: bounded file reads, repo search, index lookup, and `git`
//! status/diff. The skill set is intentionally read-only — write/execute
//! actions go through the [`crate::ApprovalGate`] proposal flow instead.

mod read_only;
mod registry;
mod tool_runtime;
mod types;

pub use read_only::{resolve_approved_repo_root, ResolveError};
pub use registry::{Registry, SkillError};
pub use tool_runtime::{
    list_read_only_tools, DispatchError, ExecuteRequest, ExecuteResult, ReadOnlyTool,
    TOOL_NAME_GIT_DIFF, TOOL_NAME_GIT_STATUS, TOOL_NAME_INDEX_LOOKUP, TOOL_NAME_READ_FILES,
    TOOL_NAME_SEARCH_REPO, UNKNOWN_READ_ONLY_TOOL,
};
pub use types::{
    FileContent, GitDiffRequest, GitDiffResult, GitStatusRequest, GitStatusResult,
    IndexLookupRequest, IndexLookupResult, IndexMatch, ReadFilesRequest, ReadFilesResult,
    RepoMatch, SearchRepoRequest, SearchRepoResult,
};
