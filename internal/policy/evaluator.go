// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/rillanai/rillan/internal/config"
)

type DefaultEvaluator struct{}

func NewEvaluator() *DefaultEvaluator {
	return &DefaultEvaluator{}
}

func (e *DefaultEvaluator) Evaluate(_ context.Context, input EvaluationInput) (EvaluationResult, error) {
	runtimePolicy := input.Runtime
	if runtimePolicy.Project.Name == "" && runtimePolicy.Project.Classification == "" {
		runtimePolicy = MergeRuntimePolicy(nil, input.Project)
	}
	phase := input.Phase
	if phase == "" {
		phase = EvaluationPhaseEgress
	}

	result := EvaluationResult{
		Verdict:  VerdictAllow,
		Reason:   "policy_allow",
		Request:  input.Request,
		Body:     append([]byte(nil), input.Body...),
		Findings: append([]Finding(nil), input.Scan.Findings...),
		Trace: PolicyTrace{
			Phase:       phase,
			RouteSource: DecisionSourceDefault,
		},
		Retrieval: RetrievalPlan{Source: DecisionSourceDefault},
	}

	classification := normalizePolicyString(runtimePolicy.Project.Classification)
	if classification == "" {
		classification = config.ProjectClassificationOpenSource
	}

	if input.Scan.HasBlockingFindings {
		result.Verdict = VerdictBlock
		result.Reason = "secret_scan_block"
		result.Trace.RouteSource = DecisionSourceScan
		if len(input.Scan.RedactedBody) > 0 {
			if err := syncRequestFromBody(&result, input.Scan.RedactedBody); err != nil {
				return EvaluationResult{}, err
			}
		}
		return result, nil
	}

	if runtimePolicy.ForceLocalForTradeSecret && input.Classification != nil && input.Classification.Sensitivity == SensitivityTradeSecret {
		result.Verdict = VerdictLocalOnly
		result.Reason = "system_trade_secret"
		result.Trace.RouteSource = runtimePolicy.Trace.ForceLocalForTradeSecretSource
		if len(input.Scan.RedactedBody) > 0 && len(input.Scan.Findings) > 0 {
			if err := syncRequestFromBody(&result, input.Scan.RedactedBody); err != nil {
				return EvaluationResult{}, err
			}
		}
		return result, nil
	}

	if input.Classification != nil && input.Classification.Sensitivity == SensitivityTradeSecret {
		result.Verdict = VerdictLocalOnly
		result.Reason = "classifier_trade_secret"
		result.Trace.RouteSource = DecisionSourceClassifier
		if len(input.Scan.RedactedBody) > 0 && len(input.Scan.Findings) > 0 {
			if err := syncRequestFromBody(&result, input.Scan.RedactedBody); err != nil {
				return EvaluationResult{}, err
			}
		}
		return result, nil
	}

	if classification == config.ProjectClassificationTradeSecret {
		result.Verdict = VerdictLocalOnly
		result.Reason = "project_trade_secret"
		result.Trace.RouteSource = runtimePolicy.Trace.ProjectClassificationSource
		if len(input.Scan.RedactedBody) > 0 && len(input.Scan.Findings) > 0 {
			if err := syncRequestFromBody(&result, input.Scan.RedactedBody); err != nil {
				return EvaluationResult{}, err
			}
		}
		return result, nil
	}

	if len(input.Scan.Findings) > 0 {
		result.Verdict = VerdictRedact
		result.Reason = "secret_scan_redact"
		result.Trace.RouteSource = DecisionSourceScan
		if err := syncRequestFromBody(&result, input.Scan.RedactedBody); err != nil {
			return EvaluationResult{}, err
		}
		return result, nil
	}

	if phase == EvaluationPhasePreflight && runtimePolicy.MinimizeRemoteContext {
		result.Retrieval = RetrievalPlan{
			Apply:           true,
			TopKCap:         runtimePolicy.RemoteRetrievalTopK,
			MaxContextChars: runtimePolicy.RemoteMaxContextChars,
			Source:          DecisionSourceDefault,
		}
	}

	return result, nil
}

func normalizePolicyString(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func syncRequestFromBody(result *EvaluationResult, body []byte) error {
	result.Body = append([]byte(nil), body...)

	var request = result.Request
	if err := json.Unmarshal(body, &request); err != nil {
		return err
	}
	result.Request = request
	return nil
}
