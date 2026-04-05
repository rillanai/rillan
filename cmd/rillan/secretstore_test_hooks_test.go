package main

import (
	keyring "github.com/zalando/go-keyring"

	"github.com/rillanai/rillan/internal/secretstore"
)

func secretstoreKeyringSet(fn func(service string, user string, password string) error) {
	secretstore.SetKeyringSetForTest(fn)
}

func secretstoreKeyringGet(fn func(service string, user string) (string, error)) {
	secretstore.SetKeyringGetForTest(fn)
}

func secretstoreKeyringDelete(fn func(service string, user string) error) {
	secretstore.SetKeyringDeleteForTest(fn)
}

func resetSecretstoreTestHooks() {
	secretstore.SetKeyringSetForTest(keyring.Set)
	secretstore.SetKeyringGetForTest(keyring.Get)
	secretstore.SetKeyringDeleteForTest(keyring.Delete)
}

func secretstoreErrNotFound() error {
	return keyring.ErrNotFound
}
