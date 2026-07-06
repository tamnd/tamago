package gen

import (
	"fmt"
	"strings"

	"github.com/tamnd/tamago/pkg/spec"
)

// Claude renders a Claude Code subagent definition (.claude/agents/NAME.md).
type Claude struct{}

func (Claude) Name() string { return "claude" }

// claudeTools maps catalog tools to Claude Code tool names.
var claudeTools = map[string][]string{
	"web_search":  {"WebSearch"},
	"web_fetch":   {"WebFetch"},
	"http_get":    {"WebFetch"},
	"read_file":   {"Read"},
	"list_files":  {"Glob"},
	"grep":        {"Grep"},
	"write_file":  {"Write", "Edit"},
	"bash":        {"Bash"},
	"http_post":   {"Bash"},
	"git_commit":  {"Bash"},
	"git_push":    {"Bash"},
	"deploy":      {"Bash"},
	"send_email":  {"Bash"},
	"delete_file": {"Bash"},
}

// claudeModel maps tiers to Claude Code model keywords.
var claudeModel = map[string]string{
	spec.TierFast:     "haiku",
	spec.TierStandard: "sonnet",
	spec.TierDeep:     "opus",
}

func (Claude) Generate(s *spec.AgentSpec) (map[string]string, error) {
	seen := map[string]bool{}
	var tools []string
	for _, t := range s.Tools {
		for _, ct := range claudeTools[t] {
			if !seen[ct] {
				seen[ct] = true
				tools = append(tools, ct)
			}
		}
	}
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", s.Name)
	fmt.Fprintf(&b, "description: %s\n", strings.ReplaceAll(s.Job, "\n", " "))
	if len(tools) > 0 {
		fmt.Fprintf(&b, "tools: %s\n", strings.Join(tools, ", "))
	}
	fmt.Fprintf(&b, "model: %s\n", claudeModel[s.Tier])
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimSpace(s.SystemPrompt))
	b.WriteString("\n")
	if s.Escalation != "" {
		fmt.Fprintf(&b, "\nEscalate to a human when: %s\n", s.Escalation)
	}
	path := fmt.Sprintf(".claude/agents/%s.md", s.Name)
	return map[string]string{path: b.String()}, nil
}
