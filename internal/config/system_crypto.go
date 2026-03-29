package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	keyring "github.com/zalando/go-keyring"
)

var systemKeyringGet = keyring.Get

func decryptSystemPolicy(cfg *SystemConfig) error {
	secret, err := systemKeyringGet(cfg.Encryption.KeyringService, cfg.Encryption.KeyringAccount)
	if err != nil {
		return fmt.Errorf("read system keyring secret: %w", err)
	}

	key, err := hex.DecodeString(strings.TrimSpace(secret))
	if err != nil {
		return fmt.Errorf("decode system keyring secret: %w", err)
	}
	if len(key) != 32 {
		return fmt.Errorf("system keyring secret must decode to 32 bytes, got %d", len(key))
	}

	payload, err := base64.StdEncoding.DecodeString(strings.TrimSpace(cfg.EncryptedPayload))
	if err != nil {
		return fmt.Errorf("decode encrypted system payload: %w", err)
	}

	policy, err := decryptSystemPolicyPayload(payload, key)
	if err != nil {
		return err
	}
	cfg.Policy = policy
	return nil
}

func decryptSystemPolicyPayload(payload []byte, key []byte) (SystemPolicy, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return SystemPolicy{}, fmt.Errorf("create aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return SystemPolicy{}, fmt.Errorf("create aes-gcm cipher: %w", err)
	}
	if len(payload) < gcm.NonceSize() {
		return SystemPolicy{}, fmt.Errorf("encrypted system payload too short")
	}

	nonce := payload[:gcm.NonceSize()]
	ciphertext := payload[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return SystemPolicy{}, fmt.Errorf("decrypt system payload: %w", err)
	}

	var policy SystemPolicy
	if err := json.Unmarshal(plaintext, &policy); err != nil {
		return SystemPolicy{}, fmt.Errorf("decode system policy payload: %w", err)
	}
	return policy, nil
}

func encryptSystemPolicyPayload(policy SystemPolicy, key []byte, random io.Reader) (string, error) {
	plaintext, err := json.Marshal(policy)
	if err != nil {
		return "", fmt.Errorf("marshal system policy payload: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create aes-gcm cipher: %w", err)
	}
	if random == nil {
		random = rand.Reader
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(random, nonce); err != nil {
		return "", fmt.Errorf("read nonce: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	combined := append(nonce, ciphertext...)
	return base64.StdEncoding.EncodeToString(combined), nil
}
