// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package ollama

import "testing"

func TestNewUsesBoundedDefaultClientTimeout(t *testing.T) {
	t.Parallel()

	client := New("http://127.0.0.1:11434", nil)
	if client.httpClient == nil {
		t.Fatal("expected httpClient to be set")
	}
	if client.httpClient.Timeout <= 0 {
		t.Fatalf("httpClient timeout = %v, want > 0", client.httpClient.Timeout)
	}
}
