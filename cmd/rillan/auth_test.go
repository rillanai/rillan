// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/rillanai/rillan/internal/config"
	"github.com/rillanai/rillan/internal/secretstore"
)

func TestAuthLoginStatusAndLogout(t *testing.T) {
	store := map[string]string{}
	secretstoreTestHooks(t, store)
	configPath := tempConfigPath(t)

	login := newAuthCommand()
	login.SetArgs([]string{"--config", configPath, "login", "--endpoint", "https://team.example", "--auth-strategy", "device_oidc", "--access-token", "token-1", "--issuer", "issuer-a"})
	if err := login.Execute(); err != nil {
		t.Fatalf("login Execute returned error: %v", err)
	}

	status := newAuthCommand()
	status.SetArgs([]string{"--config", configPath, "status"})
	var stdout bytes.Buffer
	status.SetOut(&stdout)
	status.SetErr(&stdout)
	if err := status.Execute(); err != nil {
		t.Fatalf("status Execute returned error: %v", err)
	}
	if got := stdout.String(); !bytes.Contains([]byte(got), []byte("logged_in: true")) {
		t.Fatalf("status output = %q, want logged_in true", got)
	}

	logout := newAuthCommand()
	logout.SetArgs([]string{"--config", configPath, "logout"})
	if err := logout.Execute(); err != nil {
		t.Fatalf("logout Execute returned error: %v", err)
	}

	reloaded, err := config.LoadForEdit(configPath)
	if err != nil {
		t.Fatalf("LoadForEdit returned error: %v", err)
	}
	if secretstore.Exists(reloaded.Auth.Rillan.SessionRef) {
		t.Fatal("expected control-plane credential to be removed")
	}
}

func TestAuthLoginNotifiesDaemonRefresh(t *testing.T) {
	store := map[string]string{}
	secretstoreTestHooks(t, store)
	configPath := tempConfigPath(t)
	called := 0
	daemonRefreshNotifier = func(config.Config) error {
		called++
		return nil
	}
	t.Cleanup(func() { daemonRefreshNotifier = notifyDaemonRuntimeRefresh })

	login := newAuthCommand()
	login.SetArgs([]string{"--config", configPath, "login", "--endpoint", "https://team.example", "--auth-strategy", "device_oidc", "--access-token", "token-1", "--issuer", "issuer-a"})
	if err := login.Execute(); err != nil {
		t.Fatalf("login Execute returned error: %v", err)
	}
	if got, want := called, 1; got != want {
		t.Fatalf("daemon refresh calls = %d, want %d", got, want)
	}
}

func tempConfigPath(t *testing.T) string {
	t.Helper()
	return t.TempDir() + "/config.yaml"
}

func secretstoreTestHooks(t *testing.T, store map[string]string) {
	t.Helper()
	secretstoreKeyringSet(func(service string, user string, password string) error {
		store[fmt.Sprintf("%s/%s", service, user)] = password
		return nil
	})
	secretstoreKeyringGet(func(service string, user string) (string, error) {
		value, ok := store[fmt.Sprintf("%s/%s", service, user)]
		if !ok {
			return "", secretstoreErrNotFound()
		}
		return value, nil
	})
	secretstoreKeyringDelete(func(service string, user string) error {
		delete(store, fmt.Sprintf("%s/%s", service, user))
		return nil
	})
	t.Cleanup(resetSecretstoreTestHooks)
}
