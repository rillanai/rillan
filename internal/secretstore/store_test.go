// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package secretstore

import (
	"errors"
	"fmt"
	"testing"

	keyring "github.com/zalando/go-keyring"
)

func TestSaveLoadAndDeleteCredential(t *testing.T) {
	store := map[string]string{}
	keyringSet = func(service string, user string, password string) error {
		store[fmt.Sprintf("%s/%s", service, user)] = password
		return nil
	}
	keyringGet = func(service string, user string) (string, error) {
		value, ok := store[fmt.Sprintf("%s/%s", service, user)]
		if !ok {
			return "", errNotFound
		}
		return value, nil
	}
	keyringDelete = func(service string, user string) error {
		delete(store, fmt.Sprintf("%s/%s", service, user))
		return nil
	}
	t.Cleanup(func() {
		keyringSet = keyring.Set
		keyringGet = keyring.Get
		keyringDelete = keyring.Delete
	})

	ref := "keyring://rillan/llm/work-gpt"
	if err := Save(ref, Credential{Kind: "api_key", APIKey: "secret", Endpoint: "https://api.openai.com/v1", AuthStrategy: "api_key"}); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	credential, err := Load(ref)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if credential.APIKey != "secret" {
		t.Fatalf("api key = %q, want secret", credential.APIKey)
	}
	if err := Delete(ref); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if Exists(ref) {
		t.Fatal("expected credential to be deleted")
	}
}

func TestResolveBearerRejectsBindingMismatch(t *testing.T) {
	store := map[string]string{}
	keyringSet = func(service string, user string, password string) error {
		store[fmt.Sprintf("%s/%s", service, user)] = password
		return nil
	}
	keyringGet = func(service string, user string) (string, error) {
		value, ok := store[fmt.Sprintf("%s/%s", service, user)]
		if !ok {
			return "", errNotFound
		}
		return value, nil
	}
	t.Cleanup(func() {
		keyringSet = keyring.Set
		keyringGet = keyring.Get
	})

	ref := "keyring://rillan/llm/work-gpt"
	if err := Save(ref, Credential{Kind: "oidc", AccessToken: "token", Endpoint: "https://api.openai.com/v1", AuthStrategy: "browser_oidc", Issuer: "issuer-a"}); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	if _, err := ResolveBearer(ref, Binding{Endpoint: "https://api.openai.com/v1", AuthStrategy: "browser_oidc", Issuer: "issuer-b"}); err == nil {
		t.Fatal("expected ResolveBearer to reject issuer mismatch")
	}
}

func TestLoadReturnsNotFound(t *testing.T) {
	keyringGet = func(service string, user string) (string, error) {
		return "", errNotFound
	}
	t.Cleanup(func() {
		keyringGet = keyring.Get
	})

	_, err := Load("keyring://rillan/auth/team-default")
	if !errors.Is(err, errNotFound) {
		t.Fatalf("Load error = %v, want errNotFound", err)
	}
}
