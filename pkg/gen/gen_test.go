package gen

import (
	"strings"
	"testing"

	"github.com/tamnd/tamago/pkg/spec"
)

func sample() *spec.AgentSpec {
	return &spec.AgentSpec{
		Name:         "changelog-scribe",
		Job:          "summarize merged PRs into a weekly changelog",
		Role:         "a careful release-notes writer",
		Risk:         spec.RiskRead,
		Tier:         spec.TierStandard,
		SystemPrompt: "You write changelogs from merged PRs.",
		Tools:        []string{"web_fetch", "read_file", "grep"},
		Triggers:     []spec.Trigger{{Type: "cron", Cron: "0 9 * * MON"}},
		Escalation:   "when a PR looks like a security fix",
		Evals: []spec.Fixture{
			{Input: "a", Expect: "b"},
			{Input: "c", Expect: "d"},
			{Input: "e", Expect: "f"},
		},
		Meta: spec.Meta{Generation: 2},
	}
}

func TestClaudeTarget(t *testing.T) {
	files, err := Claude{}.Generate(sample())
	if err != nil {
		t.Fatal(err)
	}
	md, ok := files[".claude/agents/changelog-scribe.md"]
	if !ok {
		t.Fatalf("missing subagent file, got %v", keys(files))
	}
	for _, want := range []string{"name: changelog-scribe", "model: sonnet", "WebFetch", "Read", "Grep", "You write changelogs"} {
		if !strings.Contains(md, want) {
			t.Errorf("subagent md missing %q", want)
		}
	}
}

func TestStandaloneTarget(t *testing.T) {
	files, err := Standalone{}.Generate(sample())
	if err != nil {
		t.Fatal(err)
	}
	main := files["main.go"]
	for _, want := range []string{"claude-sonnet-4-6", "anthropic.NewClient()", "NewStreaming", "You write changelogs"} {
		if !strings.Contains(main, want) {
			t.Errorf("standalone main.go missing %q", want)
		}
	}
	if !strings.Contains(files["go.mod"], "module changelog-scribe-agent") {
		t.Error("go.mod module name wrong")
	}
}

func TestCronTarget(t *testing.T) {
	files, err := Cron{}.Generate(sample())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(files["crontab.txt"], "0 9 * * MON") {
		t.Error("crontab line missing the schedule")
	}
	s := sample()
	s.Triggers = nil
	files, err = Cron{}.Generate(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Error("cron target should emit nothing without a cron trigger")
	}
}

func TestDeterministic(t *testing.T) {
	for _, tg := range All() {
		a, _ := tg.Generate(sample())
		b, _ := tg.Generate(sample())
		if len(a) != len(b) {
			t.Fatalf("%s not deterministic", tg.Name())
		}
		for k := range a {
			if a[k] != b[k] {
				t.Errorf("%s: file %s differs between runs", tg.Name(), k)
			}
		}
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
