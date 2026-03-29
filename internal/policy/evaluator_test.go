package policy

import (
	"context"
	"strings"
	"testing"

	"github.com/sidekickos/rillan/internal/config"
)

func TestDefaultEvaluatorEvaluate(t *testing.T) {
	t.Parallel()

	evaluator := NewEvaluator()
	scanner := DefaultScanner()
	tests := []struct {
		name           string
		classification string
		body           string
		classifier     *IntentClassification
		wantVerdict    Verdict
		wantReason     string
		wantContains   string
	}{
		{
			name:           "open source allows clean body",
			classification: config.ProjectClassificationOpenSource,
			body:           `{"messages":[{"role":"user","content":"hello"}]}`,
			wantVerdict:    VerdictAllow,
			wantReason:     "policy_allow",
			wantContains:   "hello",
		},
		{
			name:           "proprietary redacts secret findings",
			classification: config.ProjectClassificationProprietary,
			body:           `{"token":"sk-1234567890abcdefghijklmnop"}`,
			wantVerdict:    VerdictRedact,
			wantReason:     "secret_scan_redact",
			wantContains:   "[REDACTED OPENAI API KEY]",
		},
		{
			name:           "trade secret forces local only",
			classification: config.ProjectClassificationTradeSecret,
			body:           `{"messages":[{"role":"user","content":"ship it"}]}`,
			wantVerdict:    VerdictLocalOnly,
			wantReason:     "project_trade_secret",
			wantContains:   "ship it",
		},
		{
			name:           "blocking findings override classification",
			classification: config.ProjectClassificationOpenSource,
			body:           `{"messages":[{"role":"user","content":"-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----"}]}`,
			wantVerdict:    VerdictBlock,
			wantReason:     "secret_scan_block",
			wantContains:   "[BLOCKED PRIVATE KEY]",
		},
		{
			name:           "classifier trade secret escalates to local only",
			classification: config.ProjectClassificationInternal,
			body:           `{"messages":[{"role":"user","content":"ship it"}]}`,
			classifier:     &IntentClassification{Sensitivity: SensitivityTradeSecret},
			wantVerdict:    VerdictLocalOnly,
			wantReason:     "classifier_trade_secret",
			wantContains:   "ship it",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			scan := scanner.Scan([]byte(tt.body))
			result, err := evaluator.Evaluate(context.Background(), EvaluationInput{
				Project: config.ProjectConfig{
					Name:           "demo",
					Classification: tt.classification,
				},
				Body:           []byte(tt.body),
				Scan:           scan,
				Classification: tt.classifier,
			})
			if err != nil {
				t.Fatalf("Evaluate returned error: %v", err)
			}
			if got, want := result.Verdict, tt.wantVerdict; got != want {
				t.Fatalf("verdict = %q, want %q", got, want)
			}
			if got, want := result.Reason, tt.wantReason; got != want {
				t.Fatalf("reason = %q, want %q", got, want)
			}
			if got := string(result.Body); tt.wantContains != "" && !containsString(got, tt.wantContains) {
				t.Fatalf("body = %q, want substring %q", got, tt.wantContains)
			}
		})
	}
}

func containsString(value string, substring string) bool {
	return len(substring) == 0 || strings.Contains(value, substring)
}
