// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! Embedding encode/decode + deterministic placeholder embedding for the
//! offline indexing path. Mirrors `internal/index/vectors.go`.

use sha2::{Digest, Sha256};
use thiserror::Error;

#[derive(Debug, Error)]
pub enum VectorError {
    #[error("embedding blob length must be divisible by 4")]
    InvalidBlobLength,
}

/// Returns an 8-element f32 vector derived from a SHA-256 of `text`. The
/// values are uniformly distributed in `[0, 1)`. Matches the Go helper bit
/// for bit so embedded-mode offline indexes round-trip.
#[must_use]
pub fn placeholder_embedding(text: &str) -> Vec<f32> {
    let mut hasher = Sha256::new();
    hasher.update(text.as_bytes());
    let digest = hasher.finalize();
    (0..8)
        .map(|i| {
            let start = i * 4;
            let mut buf = [0u8; 4];
            buf.copy_from_slice(&digest[start..start + 4]);
            let value = u32::from_le_bytes(buf);
            (value % 1000) as f32 / 1000.0
        })
        .collect()
}

/// Encodes a vector as a tightly-packed little-endian f32 blob.
#[must_use]
pub fn encode_embedding(values: &[f32]) -> Vec<u8> {
    let mut blob = Vec::with_capacity(values.len() * 4);
    for value in values {
        blob.extend_from_slice(&value.to_le_bits().to_le_bytes());
    }
    blob
}

/// Decodes a packed little-endian f32 blob back into a vector.
pub fn decode_embedding(blob: &[u8]) -> Result<Vec<f32>, VectorError> {
    if blob.len() % 4 != 0 {
        return Err(VectorError::InvalidBlobLength);
    }
    Ok(blob
        .chunks_exact(4)
        .map(|chunk| {
            let mut buf = [0u8; 4];
            buf.copy_from_slice(chunk);
            f32::from_bits(u32::from_le_bytes(buf))
        })
        .collect())
}

trait F32LeBits {
    fn to_le_bits(&self) -> u32;
}

impl F32LeBits for f32 {
    fn to_le_bits(&self) -> u32 {
        self.to_bits()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn placeholder_is_deterministic() {
        let a = placeholder_embedding("hello");
        let b = placeholder_embedding("hello");
        assert_eq!(a, b);
    }

    #[test]
    fn placeholder_has_eight_dims() {
        let v = placeholder_embedding("ping");
        assert_eq!(v.len(), 8);
    }

    #[test]
    fn round_trip_encode_decode() {
        let v = vec![0.1f32, -0.5, 1.25, 0.875];
        let blob = encode_embedding(&v);
        let back = decode_embedding(&blob).unwrap();
        assert_eq!(back.len(), v.len());
        for (a, b) in v.iter().zip(back.iter()) {
            assert!((a - b).abs() < 1e-6);
        }
    }

    #[test]
    fn invalid_blob_length_errors() {
        let err = decode_embedding(&[0, 1, 2]).expect_err("must fail");
        assert!(matches!(err, VectorError::InvalidBlobLength));
    }
}
