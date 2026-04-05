package policy

import (
	"context"
	"strings"
	"testing"

	"github.com/rillanai/rillan/internal/config"
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

func TestMergeRuntimePolicyClonesProjectConfig(t *testing.T) {
	t.Parallel()

	project := config.ProjectConfig{
		Name:           "demo",
		Classification: config.ProjectClassificationInternal,
		Routing: config.ProjectRoutingConfig{
			Default:   config.RoutePreferencePreferCloud,
			TaskTypes: map[string]string{"review": config.RoutePreferencePreferLocal},
		},
		Instructions: []string{"one"},
		Sources:      []config.ProjectSource{{Path: "/repo", Type: "go"}},
	}

	runtime := MergeRuntimePolicy(nil, project)
	runtime.Project.Routing.TaskTypes["review"] = config.RoutePreferenceLocalOnly
	runtime.Project.Instructions[0] = "changed"
	runtime.Project.Sources[0].Path = "/other"

	if got, want := project.Routing.TaskTypes["review"], config.RoutePreferencePreferLocal; got != want {
		t.Fatalf("original task route = %q, want %q", got, want)
	}
	if got, want := project.Instructions[0], "one"; got != want {
		t.Fatalf("original instruction = %q, want %q", got, want)
	}
	if got, want := project.Sources[0].Path, "/repo"; got != want {
		t.Fatalf("original source path = %q, want %q", got, want)
	}
}

func TestDefaultEvaluatorSystemRuleCanOverrideTierOneRouting(t *testing.T) {
	t.Parallel()

	evaluator := NewEvaluator()
	project := config.ProjectConfig{Name: "demo", Classification: config.ProjectClassificationOpenSource}
	system := &config.SystemConfig{Policy: config.SystemPolicy{Rules: config.SystemPolicyRules{ForceLocalForTradeSecret: true}}}

	result, err := evaluator.Evaluate(context.Background(), EvaluationInput{
		Runtime:        MergeRuntimePolicy(system, project),
		Body:           []byte(`{"messages":[{"role":"user","content":"ship it"}]}`),
		Classification: &IntentClassification{Sensitivity: SensitivityTradeSecret},
		Scan:           ScanResult{RedactedBody: []byte(`{"messages":[{"role":"user","content":"ship it"}]}`)},
		Phase:          EvaluationPhasePreflight,
	})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if got, want := result.Verdict, VerdictLocalOnly; got != want {
		t.Fatalf("verdict = %q, want %q", got, want)
	}
	if got, want := result.Reason, "system_trade_secret"; got != want {
		t.Fatalf("reason = %q, want %q", got, want)
	}
	if got, want := result.Trace.RouteSource, DecisionSourceSystem; got != want {
		t.Fatalf("route source = %q, want %q", got, want)
	}
}

func TestDefaultEvaluatorPreflightAppliesRetrievalMinimization(t *testing.T) {
	t.Parallel()

	evaluator := NewEvaluator()
	project := config.ProjectConfig{Name: "demo", Classification: config.ProjectClassificationOpenSource}

	result, err := evaluator.Evaluate(context.Background(), EvaluationInput{
		Runtime: MergeRuntimePolicy(nil, project),
		Body:    []byte(`{"messages":[{"role":"user","content":"explain this repo"}]}`),
		Scan:    ScanResult{RedactedBody: []byte(`{"messages":[{"role":"user","content":"explain this repo"}]}`)},
		Phase:   EvaluationPhasePreflight,
	})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if !result.Retrieval.Apply {
		t.Fatal("expected retrieval plan to apply in preflight")
	}
	if got, want := result.Retrieval.TopKCap, 2; got != want {
		t.Fatalf("retrieval top_k cap = %d, want %d", got, want)
	}
	if got, want := result.Retrieval.MaxContextChars, 1200; got != want {
		t.Fatalf("retrieval max_context_chars = %d, want %d", got, want)
	}
}

func containsString(value string, substring string) bool {
	return len(substring) == 0 || strings.Contains(value, substring)
}
