package spec

import (
	"path/filepath"
	"testing"
)

func valid() *AgentSpec {
	return &AgentSpec{
		Name:         "changelog-scribe",
		Job:          "summarize merged PRs into a weekly changelog",
		Role:         "a careful release-notes writer",
		Risk:         RiskRead,
		Tier:         TierStandard,
		SystemPrompt: "You write changelogs.",
		Tools:        []string{"web_fetch", "read_file"},
		Evals: []Fixture{
			{Input: "three PRs merged", Expect: "groups them by area"},
			{Input: "no PRs merged", Expect: "says the week was quiet"},
			{Input: "a PR with a breaking change", Expect: "flags the break prominently"},
		},
		Meta: Meta{Generation: 1},
	}
}

func TestValidateOK(t *testing.T) {
	if err := valid().Validate(); err != nil {
		t.Fatalf("valid spec rejected: %v", err)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*AgentSpec)
	}{
		{"bad name", func(s *AgentSpec) { s.Name = "Bad Name!" }},
		{"empty prompt", func(s *AgentSpec) { s.SystemPrompt = " " }},
		{"bad risk", func(s *AgentSpec) { s.Risk = "yolo" }},
		{"bad tier", func(s *AgentSpec) { s.Tier = "turbo" }},
		{"too few evals", func(s *AgentSpec) { s.Evals = s.Evals[:2] }},
		{"unknown tool", func(s *AgentSpec) { s.Tools = []string{"summon_demon"} }},
		{"cron without expr", func(s *AgentSpec) { s.Triggers = []Trigger{{Type: "cron"}} }},
	}
	for _, tc := range cases {
		s := valid()
		tc.mutate(s)
		if err := s.Validate(); err == nil {
			t.Errorf("%s: expected a validation error", tc.name)
		}
	}
}

func TestRiskGate(t *testing.T) {
	s := valid()
	s.Tools = append(s.Tools, "git_push") // admin tool on a read spec
	if err := s.Validate(); err == nil {
		t.Fatal("read-class spec with git_push should be refused")
	}
	s.Risk = RiskAdmin
	if err := s.Validate(); err != nil {
		t.Fatalf("admin-class spec should allow git_push: %v", err)
	}
}

func TestRoundTrip(t *testing.T) {
	s := valid()
	path := filepath.Join(t.TempDir(), "spec.yaml")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != s.Name || got.SystemPrompt != s.SystemPrompt || len(got.Evals) != 3 {
		t.Fatalf("round trip lost data: %+v", got)
	}
}

func TestTierModel(t *testing.T) {
	s := valid()
	if s.Model() != "claude-sonnet-4-6" {
		t.Fatalf("standard tier should map to sonnet, got %s", s.Model())
	}
	s.Tier = TierDeep
	if s.Model() != "claude-opus-4-8" {
		t.Fatalf("deep tier should map to opus, got %s", s.Model())
	}
}
