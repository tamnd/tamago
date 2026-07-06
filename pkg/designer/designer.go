// Package designer turns a one-line job description into a complete
// AgentSpec by asking the model to design the agent: role, system prompt,
// tool allowlist, risk class, tier, triggers, and eval fixtures.
package designer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tamnd/tamago/pkg/llm"
	"github.com/tamnd/tamago/pkg/spec"
	"gopkg.in/yaml.v3"
)

const systemPrompt = `You design AI agents. Given a job description, produce a complete agent spec.

Rules:
- name: a short lowercase slug (a-z, 0-9, hyphens) that names the agent after its job.
- role: one paragraph describing who the agent is and what good output looks like.
- system_prompt: the full production system prompt the agent will run with.
  Make it concrete and specific to the job: output format, tone, what to refuse,
  when to say it cannot do something. This is the most important field.
- risk: the smallest class that covers the job. read = only looks things up.
  write = edits files or runs commands. admin = pushes, deploys, sends, deletes.
- tools: pick ONLY from the catalog, and only tools the job actually needs.
  Every tool has a risk floor; your risk class must cover every tool you pick.
- tier: fast for mechanical transforms, standard for most jobs, deep for hard
  reasoning or high-stakes output.
- escalation: one sentence saying when the agent must stop and hand off to a human.
- evals: 3 to 5 fixtures. input is a realistic task the agent would receive,
  expect is the concrete criteria a judge can score the output against.
  Make the fixtures diverse: one happy path, one edge case, one that tests the
  refusal or escalation behavior.
- triggers: manual unless the job clearly implies a schedule, then add a cron trigger.`

// designJSON is what the model returns; meta is filled in locally.
type designJSON struct {
	Name         string         `json:"name"`
	Role         string         `json:"role"`
	Risk         string         `json:"risk"`
	Tier         string         `json:"tier"`
	SystemPrompt string         `json:"system_prompt"`
	Tools        []string       `json:"tools"`
	Escalation   string         `json:"escalation"`
	Triggers     []spec.Trigger `json:"triggers"`
	Evals        []spec.Fixture `json:"evals"`
}

func schema() map[string]any {
	str := func(desc string) map[string]any {
		return map[string]any{"type": "string", "description": desc}
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":          str("short lowercase slug"),
			"role":          str("one paragraph role description"),
			"risk":          map[string]any{"type": "string", "enum": []string{"read", "write", "admin"}},
			"tier":          map[string]any{"type": "string", "enum": []string{"fast", "standard", "deep"}},
			"system_prompt": str("full production system prompt"),
			"tools": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string", "enum": spec.CatalogNames()},
			},
			"escalation": str("when to hand off to a human"),
			"triggers": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"type": map[string]any{"type": "string", "enum": []string{"manual", "cron"}},
						"cron": str("cron expression, only for cron triggers"),
					},
					"required":             []string{"type"},
					"additionalProperties": false,
				},
			},
			"evals": map[string]any{
				"type":     "array",
				"minItems": 3,
				"maxItems": 5,
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"input":  str("realistic task input"),
						"expect": str("criteria a judge scores against"),
					},
					"required":             []string{"input", "expect"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"name", "role", "risk", "tier", "system_prompt", "tools", "escalation", "triggers", "evals"},
		"additionalProperties": false,
	}
}

// Options tune a design run.
type Options struct {
	Name   string          // override the model's name choice
	Parent *spec.AgentSpec // lineage: design a variant of an existing agent
}

// Design asks the model for an agent spec and validates it, retrying once
// with the validation error when the first attempt is off.
func Design(ctx context.Context, cl *llm.Client, job string, opts Options, onDelta func(string)) (*spec.AgentSpec, error) {
	var user strings.Builder
	fmt.Fprintf(&user, "Tool catalog with risk floors:\n")
	for _, n := range spec.CatalogNames() {
		fmt.Fprintf(&user, "- %s (%s)\n", n, spec.ToolCatalog[n])
	}
	if opts.Parent != nil {
		py, _ := yaml.Marshal(opts.Parent)
		fmt.Fprintf(&user, "\nDesign a variant of this existing agent, keeping what works:\n%s\n", py)
	}
	fmt.Fprintf(&user, "\nJob: %s\n", job)

	prompt := user.String()
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			prompt += fmt.Sprintf("\nYour previous design was rejected: %v\nFix that and return the corrected spec.", lastErr)
		}
		var d designJSON
		if err := cl.JSON(ctx, string(llm.DesignerModel), systemPrompt, prompt, 16000, schema(), &d, onDelta); err != nil {
			return nil, err
		}
		s := &spec.AgentSpec{
			Name:         d.Name,
			Job:          job,
			Role:         d.Role,
			Risk:         d.Risk,
			Tier:         d.Tier,
			SystemPrompt: d.SystemPrompt,
			Tools:        d.Tools,
			Triggers:     d.Triggers,
			Escalation:   d.Escalation,
			Evals:        d.Evals,
			Meta: spec.Meta{
				Generation: 1,
				Created:    time.Now().UTC().Format(time.RFC3339),
				Model:      string(llm.DesignerModel),
			},
		}
		if opts.Name != "" {
			s.Name = opts.Name
		}
		if opts.Parent != nil {
			s.Meta.Parent = opts.Parent.Name
		}
		if err := s.Validate(); err != nil {
			lastErr = err
			continue
		}
		return s, nil
	}
	return nil, fmt.Errorf("designer could not produce a valid spec: %w", lastErr)
}
