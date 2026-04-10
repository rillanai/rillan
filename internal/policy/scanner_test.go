// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"bytes"
	"reflect"
	"testing"
)

func TestScannerScan(t *testing.T) {
	t.Parallel()

	scanner := DefaultScanner()
	tests := []struct {
		name             string
		body             string
		wantFindings     int
		wantBlocking     bool
		wantBodyContains []string
		wantBodyMissing  []string
	}{
		{
			name:             "redacts known tokens",
			body:             `{"token":"sk-1234567890abcdefghijklmnop","auth":"Bearer abcdefghijklmnopqrstuvwxyz123456"}`,
			wantFindings:     2,
			wantBlocking:     false,
			wantBodyContains: []string{"[REDACTED OPENAI API KEY]", "Bearer [REDACTED TOKEN]"},
			wantBodyMissing:  []string{"sk-1234567890abcdefghijklmnop", "Bearer abcdefghijklmnopqrstuvwxyz123456"},
		},
		{
			name:             "blocks private key material",
			body:             "-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----",
			wantFindings:     1,
			wantBlocking:     true,
			wantBodyContains: []string{"[BLOCKED PRIVATE KEY]"},
			wantBodyMissing:  []string{"-----BEGIN PRIVATE KEY-----"},
		},
		{
			name:             "ignores short non matching strings",
			body:             `{"token":"sk-short","auth":"Bearer short"}`,
			wantFindings:     0,
			wantBlocking:     false,
			wantBodyContains: []string{"sk-short", "Bearer short"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := scanner.Scan([]byte(tt.body))
			if got, want := len(result.Findings), tt.wantFindings; got != want {
				t.Fatalf("findings = %d, want %d", got, want)
			}
			if got, want := result.HasBlockingFindings, tt.wantBlocking; got != want {
				t.Fatalf("has blocking findings = %t, want %t", got, want)
			}

			redacted := string(result.RedactedBody)
			for _, value := range tt.wantBodyContains {
				if !bytes.Contains(result.RedactedBody, []byte(value)) {
					t.Fatalf("redacted body = %q, want substring %q", redacted, value)
				}
			}
			for _, value := range tt.wantBodyMissing {
				if bytes.Contains(result.RedactedBody, []byte(value)) {
					t.Fatalf("redacted body = %q, should not contain %q", redacted, value)
				}
			}
		})
	}
}

func TestScannerScanIsDeterministic(t *testing.T) {
	t.Parallel()

	scanner := DefaultScanner()
	body := []byte(`{"token":"ghp_abcdefghijklmnopqrstuvwxyz123456","auth":"Bearer abcdefghijklmnopqrstuvwxyz123456"}`)

	first := scanner.Scan(body)
	second := scanner.Scan(body)

	if !reflect.DeepEqual(first.Findings, second.Findings) {
		t.Fatalf("findings differ between scans: %#v vs %#v", first.Findings, second.Findings)
	}
	if !bytes.Equal(first.RedactedBody, second.RedactedBody) {
		t.Fatalf("redacted bodies differ between scans: %q vs %q", string(first.RedactedBody), string(second.RedactedBody))
	}
}
