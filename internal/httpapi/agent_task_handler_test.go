package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rillanai/rillan/internal/agent"
	"github.com/rillanai/rillan/internal/audit"
)

func TestAgentTaskHandlerReturnsOrchestrationResult(t *testing.T) {
	store, err := audit.NewStore(filepath.Join(t.TempDir(), "audit", "ledger.jsonl"))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}
	handler := NewAgentTaskHandler(nil, agent.NewApprovalGate(store), nil, nil)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/agent/tasks", strings.NewReader(`{"goal":"review repo risk","execution_mode":"plan_first"}`))

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	var response AgentTaskResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if got, want := string(response.Result.Role), "orchestrator"; got != want {
		t.Fatalf("role = %q, want %q", got, want)
	}
	if response.Proposal != nil {
		t.Fatalf("expected no proposal, got %#v", response.Proposal)
	}
}

func TestAgentTaskHandlerReturnsProposalInsteadOfExecuting(t *testing.T) {
	store, err := audit.NewStore(filepath.Join(t.TempDir(), "audit", "ledger.jsonl"))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}
	handler := NewAgentTaskHandler(nil, agent.NewApprovalGate(store), nil, nil)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/agent/tasks", strings.NewReader(`{"goal":"patch repo","proposed_action":{"kind":"apply_patch","summary":"apply patch to repo"}}`))

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	var response AgentTaskResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if response.Proposal == nil {
		t.Fatal("expected proposal in response")
	}

	events, err := store.ReadAll(context.Background())
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if got, want := len(events), 1; got != want {
		t.Fatalf("events len = %d, want %d", got, want)
	}
	if got, want := events[0].Type, audit.EventTypeAgentProposal; got != want {
		t.Fatalf("event type = %q, want %q", got, want)
	}
}

func TestAgentTaskHandlerRejectsInvalidProposal(t *testing.T) {
	handler := NewAgentTaskHandler(nil, nil, nil, nil)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/agent/tasks", strings.NewReader(`{"goal":"patch repo","proposed_action":{"kind":"unknown","summary":"oops"}}`))

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestAgentTaskHandlerReturnsReadOnlySkillResults(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(repo, "docs", "guide.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte("bounded read-only skill output"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	handler := NewAgentTaskHandler(nil, nil, nil, []string{repo})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/agent/tasks", strings.NewReader(`{"goal":"inspect repo","repo_root":"`+repo+`","skill_invocations":[{"kind":"read_files","repo_root":"`+repo+`","paths":["docs/guide.md"]}]}`))

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	var response AgentTaskResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if got, want := len(response.Result.SkillResults), 1; got != want {
		t.Fatalf("skill results len = %d, want %d", got, want)
	}
	if got, want := response.Result.SkillResults[0].Kind, string(agent.SkillKindReadFiles); got != want {
		t.Fatalf("skill result kind = %q, want %q", got, want)
	}
}

func TestAgentTaskHandlerRejectsUnapprovedRepoRoot(t *testing.T) {
	repo := t.TempDir()
	handler := NewAgentTaskHandler(nil, nil, nil, nil)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/agent/tasks", strings.NewReader(`{"goal":"inspect repo","repo_root":"`+repo+`"}`))

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestAgentTaskHandlerRejectsUnapprovedSkillInvocationRepoRoot(t *testing.T) {
	repo := t.TempDir()
	handler := NewAgentTaskHandler(nil, nil, nil, nil)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/agent/tasks", strings.NewReader(`{"goal":"inspect repo","skill_invocations":[{"kind":"read_files","repo_root":"`+repo+`","paths":["docs/guide.md"]}]}`))

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestAgentTaskHandlerAcceptsOptionalMCPSnapshot(t *testing.T) {
	handler := NewAgentTaskHandler(nil, nil, nil, nil)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/agent/tasks", strings.NewReader(`{"goal":"inspect editor state","mcp_snapshot":{"open_files":[{"path":"internal/httpapi/chat_completions_handler.go"}],"diagnostics":[{"path":"internal/httpapi/chat_completions_handler.go","severity":"warning","message":"example"}]}}`))

	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestAgentProposalHandlerApprovesPendingProposal(t *testing.T) {
	store, err := audit.NewStore(filepath.Join(t.TempDir(), "audit", "ledger.jsonl"))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}
	gate := agent.NewApprovalGate(store)
	taskHandler := NewAgentTaskHandler(nil, gate, nil, nil)
	proposalHandler := NewAgentProposalHandler(nil, gate)

	taskRecorder := httptest.NewRecorder()
	taskRequest := httptest.NewRequest(http.MethodPost, "/v1/agent/tasks", strings.NewReader(`{"goal":"patch repo","proposed_action":{"kind":"apply_patch","summary":"apply patch to repo"}}`))
	taskHandler.ServeHTTP(taskRecorder, taskRequest)
	var taskResponse AgentTaskResponse
	if err := json.Unmarshal(taskRecorder.Body.Bytes(), &taskResponse); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if taskResponse.Proposal == nil {
		t.Fatal("expected proposal in task response")
	}

	decisionRecorder := httptest.NewRecorder()
	decisionRequest := httptest.NewRequest(http.MethodPost, "/v1/agent/proposals/"+taskResponse.Proposal.ID+"/decision", strings.NewReader(`{"approved":true}`))
	proposalHandler.ServeHTTP(decisionRecorder, decisionRequest)

	if got, want := decisionRecorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	var proposal AgentActionProposal
	if err := json.Unmarshal(decisionRecorder.Body.Bytes(), &proposal); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if got, want := proposal.Status, "approved"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
}

func TestAgentProposalHandlerDeniesPendingProposal(t *testing.T) {
	store, err := audit.NewStore(filepath.Join(t.TempDir(), "audit", "ledger.jsonl"))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}
	gate := agent.NewApprovalGate(store)
	taskHandler := NewAgentTaskHandler(nil, gate, nil, nil)
	proposalHandler := NewAgentProposalHandler(nil, gate)

	taskRecorder := httptest.NewRecorder()
	taskRequest := httptest.NewRequest(http.MethodPost, "/v1/agent/tasks", strings.NewReader(`{"goal":"patch repo","proposed_action":{"kind":"run_tests","summary":"run targeted tests"}}`))
	taskHandler.ServeHTTP(taskRecorder, taskRequest)
	var taskResponse AgentTaskResponse
	if err := json.Unmarshal(taskRecorder.Body.Bytes(), &taskResponse); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	decisionRecorder := httptest.NewRecorder()
	decisionRequest := httptest.NewRequest(http.MethodPost, "/v1/agent/proposals/"+taskResponse.Proposal.ID+"/decision", strings.NewReader(`{"approved":false}`))
	proposalHandler.ServeHTTP(decisionRecorder, decisionRequest)

	if got, want := decisionRecorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	var proposal AgentActionProposal
	if err := json.Unmarshal(decisionRecorder.Body.Bytes(), &proposal); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if got, want := proposal.Status, "denied"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
}
