// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: Apache-2.0

//! File discovery. Mirrors `internal/index/discovery.go`.
//!
//! Walks the configured root, applies include/exclude patterns + a `.git` /
//! `node_modules` etc. blacklist, skips symlinks and binary files, and returns
//! a sorted list of [`SourceFile`] records.

use std::fs;
use std::io;
use std::path::Path;

use rillan_config::IndexConfig;
use thiserror::Error;
use walkdir::WalkDir;

use crate::types::SourceFile;

const MAX_INDEXABLE_BYTES: u64 = 1 << 20;

#[derive(Debug, Error)]
pub enum DiscoveryError {
    #[error("index root is empty")]
    RootEmpty,
    #[error("resolve index root: {0}")]
    Resolve(#[source] io::Error),
    #[error("stat index root: {0}")]
    Stat(#[source] io::Error),
    #[error("index root must be a directory")]
    NotDirectory,
    #[error("walk index root: {0}")]
    Walk(#[source] walkdir::Error),
    #[error("read indexable file {path}: {source}")]
    Read {
        path: String,
        #[source]
        source: io::Error,
    },
}

/// Discovers indexable files under `cfg.root`. The result is sorted by
/// relative path so subsequent rebuilds are deterministic.
pub fn discover_files(cfg: &IndexConfig) -> Result<Vec<SourceFile>, DiscoveryError> {
    let root = cfg.root.trim();
    if root.is_empty() {
        return Err(DiscoveryError::RootEmpty);
    }
    let abs_root = Path::new(root)
        .canonicalize()
        .map_err(DiscoveryError::Resolve)?;
    let metadata = fs::metadata(&abs_root).map_err(DiscoveryError::Stat)?;
    if !metadata.is_dir() {
        return Err(DiscoveryError::NotDirectory);
    }

    let mut files: Vec<SourceFile> = Vec::new();
    for entry in WalkDir::new(&abs_root).follow_links(false).into_iter() {
        let entry = entry.map_err(DiscoveryError::Walk)?;
        if entry.path() == abs_root {
            continue;
        }
        let rel = entry
            .path()
            .strip_prefix(&abs_root)
            .unwrap_or(entry.path())
            .to_string_lossy()
            .replace('\\', "/");

        // Symlinks are ignored entirely (mirrors Go which checks `entry.Type()&fs.ModeSymlink`).
        let file_type = entry.file_type();
        if file_type.is_symlink() {
            continue;
        }
        if file_type.is_dir() {
            let name = entry.file_name().to_string_lossy().to_string();
            if should_skip_dir(&name) || matches_pattern(&rel, &cfg.excludes) {
                // walkdir doesn't have skip_dir from inside the for loop; we
                // rely on filtering: the next entries under a skipped dir get
                // discarded by their relative-path matching too.
                continue;
            }
            continue;
        }
        if matches_pattern(&rel, &cfg.excludes) {
            continue;
        }
        if !cfg.includes.is_empty() && !matches_pattern(&rel, &cfg.includes) {
            continue;
        }
        let metadata = entry.metadata().map_err(DiscoveryError::Walk)?;
        if metadata.len() > MAX_INDEXABLE_BYTES {
            continue;
        }
        let data = fs::read(entry.path()).map_err(|err| DiscoveryError::Read {
            path: rel.clone(),
            source: err,
        })?;
        if !is_indexable_text(&data) {
            continue;
        }
        let content = normalize_content(std::str::from_utf8(&data).unwrap_or(""));
        files.push(SourceFile {
            absolute_path: entry.path().to_path_buf(),
            relative_path: rel,
            content,
            size_bytes: metadata.len(),
        });
    }
    // Apply parent-skip semantics by filtering out paths whose any segment
    // matches a skip-dir name. walkdir does not support `skip_dir` while
    // iterating, so we post-filter instead. This stays correct because a
    // skipped directory's children share a path segment with it.
    files.retain(|file| !file.relative_path.split('/').any(should_skip_dir));
    files.sort_by(|a, b| a.relative_path.cmp(&b.relative_path));
    Ok(files)
}

fn should_skip_dir(name: &str) -> bool {
    matches!(name, ".git" | "node_modules" | ".direnv" | ".idea")
}

fn matches_pattern(value: &str, patterns: &[String]) -> bool {
    let trimmed = value.trim().replace('\\', "/");
    let base = trimmed.rsplit('/').next().unwrap_or(&trimmed);
    for pattern in patterns {
        let cleaned = pattern.trim().replace('\\', "/");
        if cleaned.is_empty() {
            continue;
        }
        if trimmed == cleaned || trimmed.starts_with(&format!("{cleaned}/")) {
            return true;
        }
        if matches_glob(&cleaned, &trimmed) {
            return true;
        }
        if !cleaned.contains('/') && matches_glob(&cleaned, base) {
            return true;
        }
    }
    false
}

fn matches_glob(pattern: &str, value: &str) -> bool {
    if !pattern.contains('*') && !pattern.contains('?') && !pattern.contains('[') {
        return false;
    }
    let pattern_segments: Vec<&str> = pattern.split('/').collect();
    let value_segments: Vec<&str> = value.split('/').collect();
    match_path_segments(&pattern_segments, &value_segments)
}

fn match_path_segments(pattern_segments: &[&str], value_segments: &[&str]) -> bool {
    if pattern_segments.is_empty() {
        return value_segments.is_empty();
    }
    if pattern_segments[0] == "**" {
        if match_path_segments(&pattern_segments[1..], value_segments) {
            return true;
        }
        if value_segments.is_empty() {
            return false;
        }
        return match_path_segments(pattern_segments, &value_segments[1..]);
    }
    if value_segments.is_empty() {
        return false;
    }
    if !match_glob_segment(pattern_segments[0], value_segments[0]) {
        return false;
    }
    match_path_segments(&pattern_segments[1..], &value_segments[1..])
}

/// Single-segment glob match supporting `*`, `?`, and `[...]` character
/// classes. Mirrors Go's `path.Match` semantics for the inputs used by the Go
/// repo's tests.
fn match_glob_segment(pattern: &str, value: &str) -> bool {
    glob_match(pattern.as_bytes(), value.as_bytes())
}

fn glob_match(pattern: &[u8], value: &[u8]) -> bool {
    let mut pi = 0usize;
    let mut vi = 0usize;
    let mut star_p: Option<usize> = None;
    let mut star_v: usize = 0;
    while vi < value.len() {
        if pi < pattern.len() {
            match pattern[pi] {
                b'*' => {
                    star_p = Some(pi);
                    star_v = vi;
                    pi += 1;
                    continue;
                }
                b'?' => {
                    pi += 1;
                    vi += 1;
                    continue;
                }
                b'[' => {
                    if let Some((advance_p, matched)) = match_char_class(&pattern[pi..], value[vi])
                    {
                        if matched {
                            pi += advance_p;
                            vi += 1;
                            continue;
                        }
                    }
                    if let Some(sp) = star_p {
                        star_v += 1;
                        vi = star_v;
                        pi = sp + 1;
                        continue;
                    }
                    return false;
                }
                ch if ch == value[vi] => {
                    pi += 1;
                    vi += 1;
                    continue;
                }
                _ => {}
            }
        }
        if let Some(sp) = star_p {
            star_v += 1;
            vi = star_v;
            pi = sp + 1;
            continue;
        }
        return false;
    }
    while pi < pattern.len() && pattern[pi] == b'*' {
        pi += 1;
    }
    pi == pattern.len()
}

fn match_char_class(pattern: &[u8], value: u8) -> Option<(usize, bool)> {
    if pattern.is_empty() || pattern[0] != b'[' {
        return None;
    }
    let mut idx = 1usize;
    let mut negate = false;
    if idx < pattern.len() && pattern[idx] == b'^' {
        negate = true;
        idx += 1;
    }
    let mut matched = false;
    while idx < pattern.len() {
        if pattern[idx] == b']' && idx > 1 {
            return Some((idx + 1, matched ^ negate));
        }
        if idx + 2 < pattern.len() && pattern[idx + 1] == b'-' && pattern[idx + 2] != b']' {
            let lo = pattern[idx];
            let hi = pattern[idx + 2];
            if value >= lo && value <= hi {
                matched = true;
            }
            idx += 3;
            continue;
        }
        if pattern[idx] == value {
            matched = true;
        }
        idx += 1;
    }
    None
}

fn is_indexable_text(data: &[u8]) -> bool {
    if data.is_empty() {
        return true;
    }
    if data.contains(&0) {
        return false;
    }
    std::str::from_utf8(data).is_ok()
}

fn normalize_content(content: &str) -> String {
    content.replace("\r\n", "\n").replace('\r', "\n")
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;
    use std::path::PathBuf;

    fn write_file(path: &Path, content: &[u8]) {
        if let Some(parent) = path.parent() {
            fs::create_dir_all(parent).unwrap();
        }
        fs::write(path, content).unwrap();
    }

    #[test]
    fn returns_deterministic_order() {
        let dir = tempfile::tempdir().unwrap();
        write_file(&dir.path().join("b.txt"), b"second");
        write_file(&dir.path().join("a.txt"), b"first");
        let cfg = IndexConfig {
            root: dir.path().to_string_lossy().to_string(),
            includes: Vec::new(),
            excludes: vec![
                ".git".into(),
                "node_modules".into(),
                ".direnv".into(),
                ".idea".into(),
            ],
            chunk_size_lines: 10,
        };
        let files = discover_files(&cfg).expect("discover");
        assert_eq!(files.len(), 2);
        assert_eq!(files[0].relative_path, "a.txt");
        assert_eq!(files[1].relative_path, "b.txt");
    }

    #[test]
    fn skips_excluded_and_binary_files() {
        let dir = tempfile::tempdir().unwrap();
        write_file(&dir.path().join("keep.go"), b"package main\n");
        write_file(
            &dir.path().join("nested").join("keep.go"),
            b"package nested\n",
        );
        write_file(&dir.path().join("skip.txt"), b"skip");
        write_file(&dir.path().join("image.bin"), &[0, 1, 2]);
        let cfg = IndexConfig {
            root: dir.path().to_string_lossy().to_string(),
            includes: vec!["*.go".into()],
            excludes: vec!["skip.txt".into()],
            chunk_size_lines: 10,
        };
        let files = discover_files(&cfg).expect("discover");
        assert_eq!(files.len(), 2);
        assert_eq!(files[0].relative_path, "keep.go");
        assert_eq!(files[1].relative_path, "nested/keep.go");
    }

    #[test]
    fn supports_recursive_glob_patterns() {
        let dir = tempfile::tempdir().unwrap();
        write_file(&dir.path().join("docs").join("guide.md"), b"guide");
        write_file(
            &dir.path().join("nested").join("docs").join("notes.md"),
            b"notes",
        );
        write_file(
            &dir.path().join("nested").join("docs").join("skip.txt"),
            b"skip",
        );
        let cfg = IndexConfig {
            root: dir.path().to_string_lossy().to_string(),
            includes: vec!["**/*.md".into()],
            excludes: Vec::new(),
            chunk_size_lines: 10,
        };
        let files = discover_files(&cfg).expect("discover");
        assert_eq!(files.len(), 2);
        assert_eq!(files[0].relative_path, "docs/guide.md");
        assert_eq!(files[1].relative_path, "nested/docs/notes.md");
    }

    #[test]
    fn requires_directory_root() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("file.txt");
        write_file(&path, b"content");
        let cfg = IndexConfig {
            root: path.to_string_lossy().to_string(),
            chunk_size_lines: 10,
            ..IndexConfig::default()
        };
        let err = discover_files(&cfg).expect_err("must fail");
        assert!(matches!(err, DiscoveryError::NotDirectory));
    }

    #[test]
    fn skips_symlink_targets() {
        if cfg!(target_os = "windows") {
            return;
        }
        let dir = tempfile::tempdir().unwrap();
        let outside = tempfile::tempdir().unwrap();
        let target = outside.path().join("outside.txt");
        write_file(&target, b"outside");
        let _ = std::os::unix::fs::symlink(&target, dir.path().join("linked.txt"));
        let cfg = IndexConfig {
            root: dir.path().to_string_lossy().to_string(),
            chunk_size_lines: 10,
            ..IndexConfig::default()
        };
        let files = discover_files(&cfg).expect("discover");
        assert!(files.is_empty());
    }

    #[test]
    fn glob_match_segment_handles_ranges_and_stars() {
        assert!(super::glob_match(b"*.go", b"main.go"));
        assert!(!super::glob_match(b"*.go", b"main.rs"));
        assert!(super::glob_match(b"prefix?.txt", b"prefix1.txt"));
        assert!(super::glob_match(b"file[0-9].txt", b"file3.txt"));
        assert!(!super::glob_match(b"file[0-9].txt", b"filea.txt"));
    }

    #[test]
    fn discovery_path_round_trips() {
        let dir = tempfile::tempdir().unwrap();
        let nested = dir.path().join("a").join("b").join("c.txt");
        write_file(&nested, b"hi");
        let cfg = IndexConfig {
            root: dir.path().to_string_lossy().to_string(),
            chunk_size_lines: 10,
            ..IndexConfig::default()
        };
        let files = discover_files(&cfg).expect("discover");
        let abs: PathBuf = files[0].absolute_path.clone();
        assert!(abs.ends_with("c.txt"));
    }
}
