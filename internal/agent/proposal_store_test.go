// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package agent

import "testing"

func TestProposalStorePutGetAndUpdateStatus(t *testing.T) {
	store := NewProposalStore()
	proposal := ActionProposal{ID: "proposal-1", Kind: ActionKindApplyPatch, Summary: "apply patch", Status: "pending"}
	store.Put(proposal)

	loaded, err := store.Get("proposal-1")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got, want := loaded.Status, "pending"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	updated, err := store.UpdateStatus("proposal-1", "approved")
	if err != nil {
		t.Fatalf("UpdateStatus returned error: %v", err)
	}
	if got, want := updated.Status, "approved"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
}
