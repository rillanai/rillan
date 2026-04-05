package routing

import (
	"github.com/rillanai/rillan/internal/config"
	"github.com/rillanai/rillan/internal/policy"
)

type Location string

const (
	LocationLocal  Location = "local"
	LocationRemote Location = "remote"
)

type PreferenceSource string

const (
	PreferenceSourceDefault        PreferenceSource = "default"
	PreferenceSourceProjectDefault PreferenceSource = "project_default"
	PreferenceSourceTaskType       PreferenceSource = "task_type"
)

type ResolvedPreference struct {
	Preference string
	Source     PreferenceSource
}

type Candidate struct {
	ID           string
	Backend      string
	Preset       string
	Transport    string
	Endpoint     string
	DefaultModel string
	ModelPins    []string
	Capabilities []string
	Location     Location
}

type Catalog struct {
	Candidates []Candidate
	ByID       map[string]Candidate
	Allowed    bool
}

type DecisionInput struct {
	RequestedModel       string
	RequiredCapabilities []string
	Action               policy.ActionType
	Project              config.ProjectConfig
	PolicyVerdict        policy.Verdict
	Candidates           []Candidate
}

type Decision struct {
	Preference ResolvedPreference
	Selected   *Candidate
	Ranked     []Candidate
	Trace      DecisionTrace
}

type DecisionTrace struct {
	PolicyVerdict        policy.Verdict
	ModelTarget          string
	ModelMatch           string
	RequiredCapabilities []string
	Preference           string
	PreferenceSource     PreferenceSource
	Candidates           []CandidateTrace
}

type CandidateTrace struct {
	ID                  string
	Location            Location
	Eligible            bool
	Rejected            bool
	Selected            bool
	Reason              string
	ModelMatch          bool
	MissingCapabilities []string
	PreferenceScore     int
	TaskStrength        int
}
