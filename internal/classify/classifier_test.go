package classify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rillanai/rillan/internal/chat"
	"github.com/rillanai/rillan/internal/ollama"
	"github.com/rillanai/rillan/internal/policy"
)

func TestOllamaClassifierClassify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		response  string
		status    int
		want      *policy.IntentClassification
		wantError error
	}{
		{
			name:     "parses valid response",
			response: `{"response":"{\"action_type\":\"code_generation\",\"sensitivity\":\"internal\",\"requires_context\":true,\"execution_mode\":\"plan_first\",\"confidence\":0.92}"}`,
			status:   http.StatusOK,
			want: &policy.IntentClassification{
				Action:          policy.ActionTypeCodeGeneration,
				Sensitivity:     policy.SensitivityInternal,
				RequiresContext: true,
				ExecutionMode:   policy.ExecutionModePlanFirst,
				Confidence:      0.92,
			},
		},
		{
			name:      "rejects malformed json payload",
			response:  `{"response":"not json"}`,
			status:    http.StatusOK,
			wantError: errors.New("classifier response did not contain JSON object"),
		},
		{
			name:      "rejects partial response",
			response:  `{"response":"{\"action_type\":\"review\"}"}`,
			status:    http.StatusOK,
			wantError: errors.New("invalid sensitivity"),
		},
		{
			name:      "rejects low confidence response",
			response:  `{"response":"{\"action_type\":\"review\",\"sensitivity\":\"public\",\"requires_context\":false,\"execution_mode\":\"direct\",\"confidence\":0.2}"}`,
			status:    http.StatusOK,
			wantError: ErrLowConfidence,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			classifier := NewOllamaClassifier(ollama.New(server.URL, server.Client()), "qwen3:0.6b")
			got, err := classifier.Classify(context.Background(), testRequest(t))
			if tt.wantError != nil {
				if err == nil {
					t.Fatalf("expected error %v, got nil", tt.wantError)
				}
				if !errors.Is(err, tt.wantError) && !containsError(err, tt.wantError.Error()) {
					t.Fatalf("error = %v, want %v", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("Classify returned error: %v", err)
			}
			if *got != *tt.want {
				t.Fatalf("classification = %#v, want %#v", *got, *tt.want)
			}
		})
	}
}

func TestSafeClassifierSuppressesErrors(t *testing.T) {
	t.Parallel()

	classifier := NewSafeClassifier(failingClassifier{err: context.DeadlineExceeded})
	got, err := classifier.Classify(context.Background(), testRequest(t))
	if err != nil {
		t.Fatalf("Classify returned error: %v", err)
	}
	if got != nil {
		t.Fatalf("classification = %#v, want nil", got)
	}
}

type failingClassifier struct {
	err error
}

func (f failingClassifier) Classify(context.Context, chat.Request) (*policy.IntentClassification, error) {
	return nil, f.err
}

func testRequest(t *testing.T) chat.Request {
	t.Helper()
	content, err := json.Marshal("please review this diff")
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}
	return chat.Request{
		Model: "gpt-4o-mini",
		Messages: []chat.Message{{
			Role:    "user",
			Content: content,
		}},
	}
}

func containsError(err error, needle string) bool {
	return err != nil && needle != "" && strings.Contains(err.Error(), needle)
}
