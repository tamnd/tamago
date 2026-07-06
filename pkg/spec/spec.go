// Package spec defines the AgentSpec format: the declarative description of
// an agent that tamago designs, generates, evaluates, and grows.
package spec

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Risk classes order the blast radius an agent is allowed to have.
// A spec declares one class and may only use tools at or below it.
const (
	RiskRead  = "read"
	RiskWrite = "write"
	RiskAdmin = "admin"
)

// Tiers pick the model an agent runs on.
const (
	TierFast     = "fast"
	TierStandard = "standard"
	TierDeep     = "deep"
)

// TierModel maps a tier to the model ID the agent runs on.
var TierModel = map[string]string{
	TierFast:     "claude-haiku-4-5",
	TierStandard: "claude-sonnet-4-6",
	TierDeep:     "claude-opus-4-8",
}

// Trigger describes when an agent fires.
type Trigger struct {
	Type string `yaml:"type" json:"type"` // manual or cron
	Cron string `yaml:"cron,omitempty" json:"cron,omitempty"`
}

// Fixture is one eval case: an input the agent gets and the criteria a judge
// scores the output against.
type Fixture struct {
	Input  string `yaml:"input" json:"input"`
	Expect string `yaml:"expect" json:"expect"`
}

// Meta carries lineage and bookkeeping.
type Meta struct {
	Parent     string `yaml:"parent,omitempty" json:"parent,omitempty"`
	Generation int    `yaml:"generation" json:"generation"`
	Created    string `yaml:"created,omitempty" json:"created,omitempty"`
	Model      string `yaml:"model,omitempty" json:"model,omitempty"` // designer model that wrote this spec
}

// AgentSpec is the whole agent, declaratively.
type AgentSpec struct {
	Name         string    `yaml:"name" json:"name"`
	Job          string    `yaml:"job" json:"job"`
	Role         string    `yaml:"role" json:"role"`
	Risk         string    `yaml:"risk" json:"risk"`
	Tier         string    `yaml:"tier" json:"tier"`
	SystemPrompt string    `yaml:"system_prompt" json:"system_prompt"`
	Tools        []string  `yaml:"tools" json:"tools"`
	Triggers     []Trigger `yaml:"triggers,omitempty" json:"triggers,omitempty"`
	Escalation   string    `yaml:"escalation,omitempty" json:"escalation,omitempty"`
	Evals        []Fixture `yaml:"evals" json:"evals"`
	Meta         Meta      `yaml:"meta" json:"meta"`
}

// Model returns the model ID this agent runs on.
func (s *AgentSpec) Model() string {
	if m, ok := TierModel[s.Tier]; ok {
		return m
	}
	return TierModel[TierStandard]
}

var nameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)

// Validate checks the spec is complete and inside its declared risk class.
func (s *AgentSpec) Validate() error {
	if !nameRe.MatchString(s.Name) {
		return fmt.Errorf("name %q must be a lowercase slug (a-z, 0-9, -)", s.Name)
	}
	if strings.TrimSpace(s.Job) == "" {
		return fmt.Errorf("job is empty")
	}
	if strings.TrimSpace(s.SystemPrompt) == "" {
		return fmt.Errorf("system_prompt is empty")
	}
	switch s.Risk {
	case RiskRead, RiskWrite, RiskAdmin:
	default:
		return fmt.Errorf("risk %q must be read, write, or admin", s.Risk)
	}
	switch s.Tier {
	case TierFast, TierStandard, TierDeep:
	default:
		return fmt.Errorf("tier %q must be fast, standard, or deep", s.Tier)
	}
	if len(s.Evals) < 3 {
		return fmt.Errorf("need at least 3 eval fixtures, got %d", len(s.Evals))
	}
	for i, f := range s.Evals {
		if strings.TrimSpace(f.Input) == "" || strings.TrimSpace(f.Expect) == "" {
			return fmt.Errorf("eval fixture %d has an empty input or expect", i+1)
		}
	}
	for _, tr := range s.Triggers {
		if tr.Type != "manual" && tr.Type != "cron" {
			return fmt.Errorf("trigger type %q must be manual or cron", tr.Type)
		}
		if tr.Type == "cron" && strings.TrimSpace(tr.Cron) == "" {
			return fmt.Errorf("cron trigger needs a cron expression")
		}
	}
	return s.CheckRisk()
}

// Load reads a spec from a YAML file.
func Load(path string) (*AgentSpec, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s AgentSpec
	if err := yaml.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &s, nil
}

// Save writes the spec as YAML.
func (s *AgentSpec) Save(path string) error {
	b, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
