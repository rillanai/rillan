package policy

import (
	"context"

	"github.com/rillanai/rillan/internal/chat"
	"github.com/rillanai/rillan/internal/config"
)

type Verdict string

type EvaluationPhase string

type DecisionSource string

const (
	VerdictAllow     Verdict = "allow"
	VerdictRedact    Verdict = "redact"
	VerdictBlock     Verdict = "block"
	VerdictLocalOnly Verdict = "local_only"

	EvaluationPhasePreflight EvaluationPhase = "preflight"
	EvaluationPhaseEgress    EvaluationPhase = "egress"

	DecisionSourceDefault    DecisionSource = "default"
	DecisionSourceProject    DecisionSource = "project"
	DecisionSourceSystem     DecisionSource = "system"
	DecisionSourceClassifier DecisionSource = "classifier"
	DecisionSourceScan       DecisionSource = "scan"
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

type RuntimePolicy struct {
	Project                  config.ProjectConfig
	ForceLocalForTradeSecret bool
	MinimizeRemoteContext    bool
	RemoteRetrievalTopK      int
	RemoteMaxContextChars    int
	Trace                    RuntimePolicyTrace
}

type RuntimePolicyTrace struct {
	ProjectClassificationSource    DecisionSource
	ForceLocalForTradeSecretSource DecisionSource
}

type PolicyTrace struct {
	Phase       EvaluationPhase
	RouteSource DecisionSource
}

type RetrievalPlan struct {
	Apply           bool
	TopKCap         int
	MaxContextChars int
	Source          DecisionSource
}

type EvaluationInput struct {
	Project        config.ProjectConfig
	Runtime        RuntimePolicy
	Request        chat.Request
	Body           []byte
	Scan           ScanResult
	Classification *IntentClassification
	Phase          EvaluationPhase
}

type EvaluationResult struct {
	Verdict   Verdict
	Reason    string
	Request   chat.Request
	Body      []byte
	Findings  []Finding
	Trace     PolicyTrace
	Retrieval RetrievalPlan
}

type Evaluator interface {
	Evaluate(ctx context.Context, input EvaluationInput) (EvaluationResult, error)
}

func MergeRuntimePolicy(system *config.SystemConfig, project config.ProjectConfig) RuntimePolicy {
	runtime := RuntimePolicy{
		Project:               cloneProjectConfig(project),
		MinimizeRemoteContext: true,
		RemoteRetrievalTopK:   2,
		RemoteMaxContextChars: 1200,
		Trace: RuntimePolicyTrace{
			ProjectClassificationSource:    DecisionSourceDefault,
			ForceLocalForTradeSecretSource: DecisionSourceDefault,
		},
	}

	if runtime.Project.Classification != "" {
		runtime.Trace.ProjectClassificationSource = DecisionSourceProject
	}
	if system != nil && system.Policy.Rules.ForceLocalForTradeSecret {
		runtime.ForceLocalForTradeSecret = true
		runtime.Trace.ForceLocalForTradeSecretSource = DecisionSourceSystem
	}

	return runtime
}

func cloneProjectConfig(project config.ProjectConfig) config.ProjectConfig {
	cloned := project
	if project.Sources != nil {
		cloned.Sources = append([]config.ProjectSource(nil), project.Sources...)
	}
	if project.Instructions != nil {
		cloned.Instructions = append([]string(nil), project.Instructions...)
	}
	if project.Routing.TaskTypes != nil {
		cloned.Routing.TaskTypes = make(map[string]string, len(project.Routing.TaskTypes))
		for key, value := range project.Routing.TaskTypes {
			cloned.Routing.TaskTypes[key] = value
		}
	}
	return cloned
}
