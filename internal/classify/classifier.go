package classify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/rillanai/rillan/internal/chat"
	"github.com/rillanai/rillan/internal/ollama"
	"github.com/rillanai/rillan/internal/policy"
)

var ErrLowConfidence = errors.New("classification confidence below threshold")

const defaultMinConfidence = 0.5

const classifyPrompt = `Classify the following chat request for a local policy engine. Return only JSON with these fields:
- action_type: one of code_diagnosis, code_generation, architecture, explanation, refactor, review, general_qa
- sensitivity: one of public, internal, proprietary, trade_secret
- requires_context: boolean
- execution_mode: one of direct, plan_first
- confidence: number from 0 to 1

Request:
%s`

type Classifier interface {
	Classify(ctx context.Context, req chat.Request) (*policy.IntentClassification, error)
}

type OllamaClassifier struct {
	client        *ollama.Client
	model         string
	minConfidence float64
}

type SafeClassifier struct {
	primary Classifier
}

type rawClassification struct {
	ActionType      string  `json:"action_type"`
	Sensitivity     string  `json:"sensitivity"`
	RequiresContext bool    `json:"requires_context"`
	ExecutionMode   string  `json:"execution_mode"`
	Confidence      float64 `json:"confidence"`
}

func NewOllamaClassifier(client *ollama.Client, model string) *OllamaClassifier {
	return &OllamaClassifier{client: client, model: model, minConfidence: defaultMinConfidence}
}

func NewSafeClassifier(primary Classifier) *SafeClassifier {
	return &SafeClassifier{primary: primary}
}

func (c *OllamaClassifier) Classify(ctx context.Context, req chat.Request) (*policy.IntentClassification, error) {
	input, err := buildInput(req)
	if err != nil {
		return nil, err
	}

	response, err := c.client.Generate(ctx, c.model, fmt.Sprintf(classifyPrompt, input))
	if err != nil {
		return nil, err
	}

	classification, err := parseClassification(response)
	if err != nil {
		return nil, err
	}
	if classification.Confidence < c.minConfidence {
		return nil, ErrLowConfidence
	}

	return classification, nil
}

func (c *SafeClassifier) Classify(ctx context.Context, req chat.Request) (*policy.IntentClassification, error) {
	if c == nil || c.primary == nil {
		return nil, nil
	}

	classification, err := c.primary.Classify(ctx, req)
	if err != nil {
		return nil, nil
	}
	return classification, nil
}

func buildInput(req chat.Request) (string, error) {
	parts := make([]string, 0, len(req.Messages))
	for _, message := range req.Messages {
		content, err := chat.MessageText(message)
		if err != nil {
			return "", fmt.Errorf("read message content: %w", err)
		}
		parts = append(parts, fmt.Sprintf("%s: %s", message.Role, strings.TrimSpace(content)))
	}
	return strings.TrimSpace(strings.Join(parts, "\n")), nil
}

func parseClassification(response string) (*policy.IntentClassification, error) {
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")
	if start == -1 || end == -1 || end < start {
		return nil, fmt.Errorf("classifier response did not contain JSON object")
	}

	payload := rawClassification{}
	if err := json.Unmarshal([]byte(response[start:end+1]), &payload); err != nil {
		return nil, fmt.Errorf("parse classifier response: %w", err)
	}

	classification := &policy.IntentClassification{
		Action:          policy.ActionType(strings.TrimSpace(payload.ActionType)),
		Sensitivity:     policy.Sensitivity(strings.TrimSpace(payload.Sensitivity)),
		RequiresContext: payload.RequiresContext,
		ExecutionMode:   policy.ExecutionMode(strings.TrimSpace(payload.ExecutionMode)),
		Confidence:      payload.Confidence,
	}

	if !validActionType(classification.Action) {
		return nil, fmt.Errorf("invalid action_type %q", payload.ActionType)
	}
	if !validSensitivity(classification.Sensitivity) {
		return nil, fmt.Errorf("invalid sensitivity %q", payload.Sensitivity)
	}
	if !validExecutionMode(classification.ExecutionMode) {
		return nil, fmt.Errorf("invalid execution_mode %q", payload.ExecutionMode)
	}

	return classification, nil
}

func validActionType(value policy.ActionType) bool {
	switch value {
	case policy.ActionTypeCodeDiagnosis, policy.ActionTypeCodeGeneration, policy.ActionTypeArchitecture, policy.ActionTypeExplanation, policy.ActionTypeRefactor, policy.ActionTypeReview, policy.ActionTypeGeneralQA:
		return true
	default:
		return false
	}
}

func validSensitivity(value policy.Sensitivity) bool {
	switch value {
	case policy.SensitivityPublic, policy.SensitivityInternal, policy.SensitivityProprietary, policy.SensitivityTradeSecret:
		return true
	default:
		return false
	}
}

func validExecutionMode(value policy.ExecutionMode) bool {
	switch value {
	case policy.ExecutionModeDirect, policy.ExecutionModePlanFirst:
		return true
	default:
		return false
	}
}
