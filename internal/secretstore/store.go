package secretstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	keyring "github.com/zalando/go-keyring"
)

var (
	keyringGet    = keyring.Get
	keyringSet    = keyring.Set
	keyringDelete = keyring.Delete
	errNotFound   = keyring.ErrNotFound
)

func IsNotFound(err error) bool {
	return errors.Is(err, errNotFound)
}

// SetKeyringGetForTest overrides keyring reads in tests.
func SetKeyringGetForTest(fn func(service string, user string) (string, error)) {
	keyringGet = fn
}

// SetKeyringSetForTest overrides keyring writes in tests.
func SetKeyringSetForTest(fn func(service string, user string, password string) error) {
	keyringSet = fn
}

// SetKeyringDeleteForTest overrides keyring deletes in tests.
func SetKeyringDeleteForTest(fn func(service string, user string) error) {
	keyringDelete = fn
}

// Credential is the keyring-backed secret payload used for provider, MCP, or
// control-plane login state.
type Credential struct {
	Kind         string `json:"kind,omitempty"`
	APIKey       string `json:"api_key,omitempty"`
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
	Endpoint     string `json:"endpoint,omitempty"`
	AuthStrategy string `json:"auth_strategy,omitempty"`
	Issuer       string `json:"issuer,omitempty"`
	Audience     string `json:"audience,omitempty"`
	StoredAt     string `json:"stored_at,omitempty"`
}

// Binding describes the endpoint-specific metadata a stored credential must
// still match when it is reused.
type Binding struct {
	Endpoint     string
	AuthStrategy string
	Issuer       string
	Audience     string
}

// Save writes a credential to the keyring reference identified by ref.
func Save(ref string, credential Credential) error {
	service, account, err := parseRef(ref)
	if err != nil {
		return err
	}
	credential.StoredAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.Marshal(credential)
	if err != nil {
		return fmt.Errorf("marshal credential: %w", err)
	}
	if err := keyringSet(service, account, string(data)); err != nil {
		return fmt.Errorf("write keyring credential: %w", err)
	}
	return nil
}

// Load reads and decodes a credential from the keyring reference identified by
// ref.
func Load(ref string) (Credential, error) {
	service, account, err := parseRef(ref)
	if err != nil {
		return Credential{}, err
	}
	value, err := keyringGet(service, account)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return Credential{}, errNotFound
		}
		return Credential{}, fmt.Errorf("read keyring credential: %w", err)
	}
	var credential Credential
	if err := json.Unmarshal([]byte(value), &credential); err != nil {
		return Credential{}, fmt.Errorf("decode keyring credential: %w", err)
	}
	return credential, nil
}

func Delete(ref string) error {
	service, account, err := parseRef(ref)
	if err != nil {
		return err
	}
	if err := keyringDelete(service, account); err != nil && !errors.Is(err, errNotFound) {
		return fmt.Errorf("delete keyring credential: %w", err)
	}
	return nil
}

// Exists reports whether a credential currently exists at ref.
func Exists(ref string) bool {
	_, err := Load(ref)
	return err == nil
}

// ResolveBearer returns the token material to use for a bound provider or MCP
// entry, rejecting credentials that no longer match the expected binding.
func ResolveBearer(ref string, binding Binding) (string, error) {
	credential, err := Load(ref)
	if err != nil {
		return "", err
	}
	if err := validateBinding(credential, binding); err != nil {
		return "", err
	}
	if credential.AccessToken != "" {
		return credential.AccessToken, nil
	}
	if credential.APIKey != "" {
		return credential.APIKey, nil
	}
	return "", fmt.Errorf("credential at %s does not contain a bearer token or api key", ref)
}

func validateBinding(credential Credential, binding Binding) error {
	if binding.Endpoint != "" && credential.Endpoint != "" && credential.Endpoint != binding.Endpoint {
		return fmt.Errorf("stored credential endpoint %q does not match %q", credential.Endpoint, binding.Endpoint)
	}
	if binding.AuthStrategy != "" && credential.AuthStrategy != "" && credential.AuthStrategy != binding.AuthStrategy {
		return fmt.Errorf("stored credential auth strategy %q does not match %q", credential.AuthStrategy, binding.AuthStrategy)
	}
	if binding.Issuer != "" && credential.Issuer != "" && credential.Issuer != binding.Issuer {
		return fmt.Errorf("stored credential issuer %q does not match %q", credential.Issuer, binding.Issuer)
	}
	if binding.Audience != "" && credential.Audience != "" && credential.Audience != binding.Audience {
		return fmt.Errorf("stored credential audience %q does not match %q", credential.Audience, binding.Audience)
	}
	return nil
}

func parseRef(ref string) (string, string, error) {
	trimmed := strings.TrimSpace(ref)
	if !strings.HasPrefix(trimmed, "keyring://") {
		return "", "", fmt.Errorf("unsupported credential ref %q", ref)
	}
	value := strings.TrimPrefix(trimmed, "keyring://")
	idx := strings.LastIndex(value, "/")
	if idx <= 0 || idx == len(value)-1 {
		return "", "", fmt.Errorf("invalid keyring ref %q", ref)
	}
	return value[:idx], value[idx+1:], nil
}
