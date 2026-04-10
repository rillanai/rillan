// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package openai

import (
	"encoding/json"
	"testing"
)

func TestValidateChatCompletionRequestAcceptsMinimalRequest(t *testing.T) {
	req := ChatCompletionRequest{
		Model: "gpt-4o-mini",
		Messages: []Message{{
			Role:    "user",
			Content: mustMarshalString(t, "hello"),
		}},
	}

	if err := ValidateChatCompletionRequest(req); err != nil {
		t.Fatalf("ValidateChatCompletionRequest returned error: %v", err)
	}
}

func TestValidateChatCompletionRequestRejectsMissingModel(t *testing.T) {
	req := ChatCompletionRequest{
		Messages: []Message{{Role: "user", Content: mustMarshalString(t, "hello")}},
	}

	if err := ValidateChatCompletionRequest(req); err == nil {
		t.Fatal("expected error for missing model")
	}
}

func TestValidateChatCompletionRequestRejectsNonStringContent(t *testing.T) {
	req := ChatCompletionRequest{
		Model:    "gpt-4o-mini",
		Messages: []Message{{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"hi"}]`)}},
	}

	if err := ValidateChatCompletionRequest(req); err != nil {
		t.Fatalf("ValidateChatCompletionRequest returned error: %v", err)
	}
	encoded, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}
	if got, want := string(encoded), `{"model":"gpt-4o-mini","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`; got != want {
		t.Fatalf("encoded request = %s, want %s", got, want)
	}
}

func TestValidateChatCompletionRequestAcceptsAssistantToolCallEnvelope(t *testing.T) {
	req := ChatCompletionRequest{}
	if err := json.Unmarshal([]byte(`{"model":"gpt-4o-mini","messages":[{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]}],"tools":[{"type":"function","function":{"name":"lookup"}}],"tool_choice":"auto"}`), &req); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}

	if err := ValidateChatCompletionRequest(req); err != nil {
		t.Fatalf("ValidateChatCompletionRequest returned error: %v", err)
	}
	encoded, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}
	if got, want := string(encoded), `{"model":"gpt-4o-mini","messages":[{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]}],"tool_choice":"auto","tools":[{"type":"function","function":{"name":"lookup"}}]}`; got != want {
		t.Fatalf("encoded request = %s, want %s", got, want)
	}
}

func TestRequiredCapabilitiesDetectsTools(t *testing.T) {
	var req ChatCompletionRequest
	if err := json.Unmarshal([]byte(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}],"tools":[{"type":"function","function":{"name":"lookup"}}],"tool_choice":"auto"}`), &req); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	got := RequiredCapabilities(req)
	if len(got) != 1 || got[0] != "tool_calling" {
		t.Fatalf("required capabilities = %#v, want [tool_calling]", got)
	}
}

func TestRequiredCapabilitiesDetectsMultimodalNonTextParts(t *testing.T) {
	var req ChatCompletionRequest
	if err := json.Unmarshal([]byte(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]}]}`), &req); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	got := RequiredCapabilities(req)
	if len(got) != 1 || got[0] != "multimodal" {
		t.Fatalf("required capabilities = %#v, want [multimodal]", got)
	}
}

func TestRequiredCapabilitiesIgnoresTextOnlyStructuredContent(t *testing.T) {
	var req ChatCompletionRequest
	if err := json.Unmarshal([]byte(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":[{"type":"text","text":"ping"}]}]}`), &req); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if got := RequiredCapabilities(req); len(got) != 0 {
		t.Fatalf("required capabilities = %#v, want none", got)
	}
}

func TestValidateChatCompletionRequestRejectsInvalidRetrievalOverride(t *testing.T) {
	zero := 0
	req := ChatCompletionRequest{
		Model:    "gpt-4o-mini",
		Messages: []Message{{Role: "user", Content: mustMarshalString(t, "hello")}},
		Retrieval: &RetrievalOptions{
			TopK: &zero,
		},
	}

	if err := ValidateChatCompletionRequest(req); err == nil {
		t.Fatal("expected error for invalid retrieval override")
	}
}

func mustMarshalString(t *testing.T, value string) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}
	return data
}
