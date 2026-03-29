package audit

import (
	"context"
	"path/filepath"
	"testing"
)

func TestStoreRecordAndReadAll(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "audit", "ledger.jsonl"))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}

	event := Event{
		Type:           EventTypeRemoteEgress,
		RequestID:      "req-1",
		Provider:       "openai",
		Model:          "gpt-4o-mini",
		Verdict:        "allow",
		Reason:         "policy_allow",
		RouteSource:    "default",
		OutboundSHA256: HashBytes([]byte("payload")),
		SourceRefs:     []string{"docs/guide.md:1-2"},
		ResponseStatus: 200,
	}
	if err := store.Record(context.Background(), event); err != nil {
		t.Fatalf("Record returned error: %v", err)
	}

	events, err := store.ReadAll(context.Background())
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if got, want := len(events), 1; got != want {
		t.Fatalf("events = %d, want %d", got, want)
	}
	if got, want := events[0].RequestID, "req-1"; got != want {
		t.Fatalf("request_id = %q, want %q", got, want)
	}
	if got, want := events[0].OutboundSHA256, HashBytes([]byte("payload")); got != want {
		t.Fatalf("outbound hash = %q, want %q", got, want)
	}
}

func TestHashBytesReturnsEmptyForEmptyInput(t *testing.T) {
	if got := HashBytes(nil); got != "" {
		t.Fatalf("HashBytes(nil) = %q, want empty", got)
	}
}
