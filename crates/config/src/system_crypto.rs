// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

//! System-policy decryption. Mirrors `internal/config/system_crypto.go`.
//!
//! The system config stores the daemon's identity and policy rules as a
//! base64-encoded AES-256-GCM ciphertext under `encrypted_payload`. The
//! 32-byte key lives in the OS keyring under
//! `system.encryption.keyring_service` / `keyring_account`, hex-encoded.

use aes_gcm::aead::Aead;
use aes_gcm::{Aes256Gcm, Key, KeyInit, Nonce};
use base64::engine::general_purpose::STANDARD as BASE64;
use base64::Engine as _;
use rillan_secretstore::Store;
use thiserror::Error;

use crate::types::{SystemConfig, SystemPolicy};

#[derive(Debug, Error)]
pub enum SystemCryptoError {
    #[error("read system keyring secret: {0}")]
    Keyring(#[from] rillan_secretstore::Error),
    #[error("decode system keyring secret: {0}")]
    DecodeSecret(#[source] hex::FromHexError),
    #[error("system keyring secret must decode to 32 bytes, got {0}")]
    BadKeyLength(usize),
    #[error("decode encrypted system payload: {0}")]
    DecodePayload(#[source] base64::DecodeError),
    #[error("create aes-gcm cipher: {0}")]
    Cipher(String),
    #[error("encrypted system payload too short")]
    PayloadTooShort,
    #[error("decrypt system payload: {0}")]
    Decrypt(String),
    #[error("decode system policy payload: {0}")]
    DecodePolicy(#[source] serde_json::Error),
}

const NONCE_SIZE: usize = 12;

/// Decrypts the encrypted policy attached to `cfg`. Mutates `cfg.policy` in
/// place on success.
pub fn decrypt_system_policy(
    cfg: &mut SystemConfig,
    store: &Store,
) -> Result<(), SystemCryptoError> {
    let secret = store_secret(store, cfg)?;
    let key = decode_key(secret.trim())?;
    let payload = BASE64
        .decode(cfg.encrypted_payload.trim().as_bytes())
        .map_err(SystemCryptoError::DecodePayload)?;
    cfg.policy = decrypt_payload(&payload, &key)?;
    Ok(())
}

fn store_secret(store: &Store, cfg: &SystemConfig) -> Result<String, SystemCryptoError> {
    let keyring_ref = format!(
        "keyring://{}/{}",
        cfg.encryption.keyring_service.trim(),
        cfg.encryption.keyring_account.trim()
    );
    // Reuse the secretstore's keyring backend; the Go variant calls
    // `keyring.Get` directly, but threading through `Store` keeps the
    // test seam consistent across the daemon.
    let credential = store.load(&keyring_ref)?;
    if !credential.api_key.is_empty() {
        return Ok(credential.api_key);
    }
    if !credential.access_token.is_empty() {
        return Ok(credential.access_token);
    }
    Err(SystemCryptoError::Keyring(
        rillan_secretstore::Error::MissingBearer(keyring_ref),
    ))
}

fn decode_key(value: &str) -> Result<[u8; 32], SystemCryptoError> {
    let raw = hex::decode(value).map_err(SystemCryptoError::DecodeSecret)?;
    let len = raw.len();
    let key: [u8; 32] = raw
        .try_into()
        .map_err(|_| SystemCryptoError::BadKeyLength(len))?;
    Ok(key)
}

fn decrypt_payload(payload: &[u8], key: &[u8; 32]) -> Result<SystemPolicy, SystemCryptoError> {
    if payload.len() < NONCE_SIZE {
        return Err(SystemCryptoError::PayloadTooShort);
    }
    let cipher = Aes256Gcm::new(Key::<Aes256Gcm>::from_slice(key));
    let nonce = Nonce::from_slice(&payload[..NONCE_SIZE]);
    let ciphertext = &payload[NONCE_SIZE..];
    let plaintext = cipher
        .decrypt(nonce, ciphertext)
        .map_err(|err| SystemCryptoError::Decrypt(err.to_string()))?;
    let policy: SystemPolicy =
        serde_json::from_slice(&plaintext).map_err(SystemCryptoError::DecodePolicy)?;
    Ok(policy)
}

#[cfg(test)]
mod tests {
    use super::*;
    use aes_gcm::aead::Aead;
    use aes_gcm::AeadCore;
    use rand::rngs::OsRng;

    #[test]
    fn round_trip_decrypts_payload() {
        let key = [7u8; 32];
        let cipher = Aes256Gcm::new(Key::<Aes256Gcm>::from_slice(&key));
        let nonce = Aes256Gcm::generate_nonce(&mut OsRng);
        let policy = SystemPolicy::default();
        let plaintext = serde_json::to_vec(&policy).unwrap();
        let ciphertext = cipher.encrypt(&nonce, plaintext.as_ref()).unwrap();
        let mut payload = nonce.to_vec();
        payload.extend(ciphertext);
        let decrypted = decrypt_payload(&payload, &key).unwrap();
        assert_eq!(decrypted, policy);
    }

    #[test]
    fn rejects_short_payload() {
        let err = decrypt_payload(&[0u8; 4], &[0u8; 32]).expect_err("too short");
        assert!(matches!(err, SystemCryptoError::PayloadTooShort));
    }

    #[test]
    fn decode_key_requires_32_bytes() {
        let bytes = hex::encode([7u8; 31]);
        let err = decode_key(&bytes).expect_err("bad length");
        assert!(matches!(err, SystemCryptoError::BadKeyLength(31)));
    }
}
