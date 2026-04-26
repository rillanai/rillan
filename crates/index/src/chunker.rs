// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! Chunking. Mirrors `internal/index/chunker.go`.

use sha2::{Digest, Sha256};

use crate::types::{ChunkRecord, DocumentRecord, SourceFile};

const DEFAULT_LINES_PER_CHUNK: usize = 120;

/// Produces a [`DocumentRecord`] for `file`.
#[must_use]
pub fn build_document(file: &SourceFile) -> DocumentRecord {
    DocumentRecord {
        path: file.relative_path.clone(),
        content_hash: hash_string(&file.content),
        size_bytes: file.size_bytes,
    }
}

/// Splits `file` into deterministic chunks. Mirrors the Go `ChunkFile`
/// algorithm: lines are split on `\n`, grouped into windows of
/// `lines_per_chunk`, and each chunk's id is `sha256(path:ordinal:hash)`.
#[must_use]
pub fn chunk_file(file: &SourceFile, lines_per_chunk: usize) -> Vec<ChunkRecord> {
    let lines_per_chunk = if lines_per_chunk == 0 {
        DEFAULT_LINES_PER_CHUNK
    } else {
        lines_per_chunk
    };
    if file.content.is_empty() {
        return Vec::new();
    }
    let lines: Vec<&str> = file.content.split('\n').collect();
    let estimated_chunks = (lines.len() / lines_per_chunk) + 1;
    let mut chunks: Vec<ChunkRecord> = Vec::with_capacity(estimated_chunks);
    let mut ordinal: u32 = 0;
    let mut start = 0usize;
    while start < lines.len() {
        let end = (start + lines_per_chunk).min(lines.len());
        let content = lines[start..end].join("\n");
        if content.is_empty() {
            start += lines_per_chunk;
            continue;
        }
        let content_hash = hash_string(&content);
        let id = hash_string(&format!(
            "{}:{}:{}",
            file.relative_path, ordinal, content_hash
        ));
        chunks.push(ChunkRecord {
            id,
            document_path: file.relative_path.clone(),
            ordinal,
            start_line: u32::try_from(start + 1).unwrap_or(u32::MAX),
            end_line: u32::try_from(end).unwrap_or(u32::MAX),
            content,
            content_hash,
        });
        ordinal += 1;
        start += lines_per_chunk;
    }
    chunks
}

fn hash_string(value: &str) -> String {
    let mut hasher = Sha256::new();
    hasher.update(value.as_bytes());
    let digest = hasher.finalize();
    let mut out = String::with_capacity(digest.len() * 2);
    for byte in &digest {
        out.push_str(&format!("{byte:02x}"));
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    fn fixture(content: &str) -> SourceFile {
        SourceFile {
            absolute_path: std::path::PathBuf::from("/tmp/main.go"),
            relative_path: "main.go".into(),
            content: content.into(),
            size_bytes: content.len() as u64,
        }
    }

    #[test]
    fn chunk_document_produces_stable_chunk_ids() {
        let file = fixture("one\ntwo\nthree\nfour");
        let first = chunk_file(&file, 2);
        let second = chunk_file(&file, 2);
        assert_eq!(first.len(), 2);
        assert_eq!(second.len(), 2);
        assert_eq!(first[0].id, second[0].id);
        assert_eq!(first[1].id, second[1].id);
    }

    #[test]
    fn chunk_document_uses_configured_line_boundaries() {
        let file = fixture("one\ntwo\nthree");
        let chunks = chunk_file(&file, 2);
        assert_eq!(chunks[0].start_line, 1);
        assert_eq!(chunks[0].end_line, 2);
        assert_eq!(chunks[1].start_line, 3);
    }

    #[test]
    fn build_document_hashes_content() {
        let file = fixture("hi");
        let doc = build_document(&file);
        assert_eq!(doc.path, "main.go");
        assert_eq!(doc.size_bytes, 2);
        assert_eq!(
            doc.content_hash,
            "8f434346648f6b96df89dda901c5176b10a6d83961dd3c1ac88b59b2dc327aa4"
        );
    }
}
