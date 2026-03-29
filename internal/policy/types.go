package policy

import (
	"context"

	"github.com/sidekickos/rillan/internal/config"
	internalopenai "github.com/sidekickos/rillan/internal/openai"
)

type Verdict string

const (
	VerdictAllow     Verdict = "allow"
	VerdictRedact    Verdict = "redact"
	VerdictBlock     Verdict = "block"
	VerdictLocalOnly Verdict = "local_only"
)

type FindingAction string

const (
	FindingActionRedact FindingAction = "redact"
	FindingActionBlock  FindingAction = "block"
)

type ActionType string

const (
	ActionTypeCodeDiagnosis  ActionType = "code_diagnosis"
	ActionTypeCodeGeneration ActionType = "code_generation"
	ActionTypeArchitecture   ActionType = "architecture"
	ActionTypeExplanation    ActionType = "explanation"
	ActionTypeRefactor       ActionType = "refactor"
	ActionTypeReview         ActionType = "review"
	ActionTypeGeneralQA      ActionType = "general_qa"
)

type Sensitivity string

const (
	SensitivityPublic      Sensitivity = "public"
	SensitivityInternal    Sensitivity = "internal"
	SensitivityProprietary Sensitivity = "proprietary"
	SensitivityTradeSecret Sensitivity = "trade_secret"
)

type ExecutionMode string

const (
	ExecutionModeDirect    ExecutionMode = "direct"
	ExecutionModePlanFirst ExecutionMode = "plan_first"
)

type Finding struct {
	RuleID      string
	Category    string
	Action      FindingAction
	Start       int
	End         int
	Length      int
	Replacement string
}

type ScanResult struct {
	Findings            []Finding
	RedactedBody        []byte
	HasBlockingFindings bool
}

type IntentClassification struct {
	Action          ActionType
	Sensitivity     Sensitivity
	RequiresContext bool
	ExecutionMode   ExecutionMode
	Confidence      float64
}

type EvaluationInput struct {
	Project        config.ProjectConfig
	Request        internalopenai.ChatCompletionRequest
	Body           []byte
	Scan           ScanResult
	Classification *IntentClassification
}

type EvaluationResult struct {
	Verdict  Verdict
	Reason   string
	Request  internalopenai.ChatCompletionRequest
	Body     []byte
	Findings []Finding
}

type Evaluator interface {
	Evaluate(ctx context.Context, input EvaluationInput) (EvaluationResult, error)
}
