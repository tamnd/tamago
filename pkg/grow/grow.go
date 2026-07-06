// Package grow improves an agent: eval, rewrite the system prompt from the
// failing transcripts, save as a new generation, re-eval, keep only if better.
package grow

import (
	"context"
	"fmt"
	"time"

	"github.com/tamnd/tamago/pkg/evals"
	"github.com/tamnd/tamago/pkg/garden"
	"github.com/tamnd/tamago/pkg/llm"
	"github.com/tamnd/tamago/pkg/spec"
)

const rewriterSystem = `You improve AI agent system prompts. You get the agent's spec, its current
system prompt, and eval results with per-fixture scores and judge reasons.
Rewrite the system prompt to fix what the judge flagged while keeping what
already scores well. Stay within the same job, tools, and risk class.
Return the complete new system prompt, not a diff, plus a short note on what
you changed and why.`

func rewriteSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"system_prompt": map[string]any{"type": "string", "description": "complete rewritten system prompt"},
			"notes":         map[string]any{"type": "string", "description": "what changed and why, two sentences"},
		},
		"required":             []string{"system_prompt", "notes"},
		"additionalProperties": false,
	}
}

// RoundResult describes one grow round.
type RoundResult struct {
	Round      int
	Before     float64
	After      float64
	Kept       bool
	Notes      string
	Generation int
}

// Event reports grow progress to the caller.
type Event struct {
	Round  int
	Stage  string // "baseline", "rewrite", "candidate-eval", "verdict"
	Detail string
}

// Grow runs n improvement rounds on the agent and saves kept generations to
// the garden. It returns one result per round.
func Grow(ctx context.Context, cl llm.Client, s *spec.AgentSpec, rounds int, onEvent func(Event), onEval func(evals.Event)) ([]RoundResult, error) {
	emit := func(e Event) {
		if onEvent != nil {
			onEvent(e)
		}
	}
	var out []RoundResult
	baseline, err := latestOrRun(ctx, cl, s, emit, onEval)
	if err != nil {
		return nil, err
	}
	for round := 1; round <= rounds; round++ {
		emit(Event{Round: round, Stage: "rewrite", Detail: fmt.Sprintf("baseline %.1f", baseline.Mean)})
		user := fmt.Sprintf("Agent job: %s\nRole: %s\nTools: %v\n\nCurrent system prompt:\n%s\n\nEval results (mean %.1f):\n%s",
			s.Job, s.Role, s.Tools, s.SystemPrompt, baseline.Mean, transcript(baseline))
		var rw struct {
			SystemPrompt string `json:"system_prompt"`
			Notes        string `json:"notes"`
		}
		if err := cl.JSON(ctx, cl.DesignerModel(), rewriterSystem, user, 16000, rewriteSchema(), &rw, nil); err != nil {
			return nil, fmt.Errorf("round %d rewrite: %w", round, err)
		}
		candidate := *s
		candidate.SystemPrompt = rw.SystemPrompt
		candidate.Meta.Generation = s.Meta.Generation + 1
		candidate.Meta.Created = time.Now().UTC().Format(time.RFC3339)

		emit(Event{Round: round, Stage: "candidate-eval", Detail: rw.Notes})
		rep, err := evals.Run(ctx, cl, &candidate, onEval)
		if err != nil {
			return nil, fmt.Errorf("round %d eval: %w", round, err)
		}
		res := RoundResult{
			Round: round, Before: baseline.Mean, After: rep.Mean,
			Notes: rw.Notes, Generation: candidate.Meta.Generation,
		}
		if rep.Mean > baseline.Mean {
			res.Kept = true
			*s = candidate
			if err := garden.Save(s); err != nil {
				return nil, err
			}
			if err := evals.Persist(s.Name, rep); err != nil {
				return nil, err
			}
			baseline = rep
			emit(Event{Round: round, Stage: "verdict",
				Detail: fmt.Sprintf("kept gen %d: %.1f -> %.1f", res.Generation, res.Before, res.After)})
		} else {
			emit(Event{Round: round, Stage: "verdict",
				Detail: fmt.Sprintf("dropped: %.1f -> %.1f, keeping gen %d", res.Before, res.After, s.Meta.Generation)})
		}
		out = append(out, res)
	}
	return out, nil
}

// latestOrRun reuses the latest stored eval for this generation, or runs one.
func latestOrRun(ctx context.Context, cl llm.Client, s *spec.AgentSpec, emit func(Event), onEval func(evals.Event)) (*evals.Report, error) {
	if last, err := garden.LatestEval(s.Name); err == nil && last != nil && last.Generation == s.Meta.Generation {
		rep := &evals.Report{Generation: last.Generation, When: last.When, Mean: last.Mean, Pass: last.Pass}
		for _, f := range last.Fixtures {
			rep.Fixtures = append(rep.Fixtures, evals.FixtureResult(f))
		}
		emit(Event{Round: 0, Stage: "baseline", Detail: fmt.Sprintf("reusing stored eval, mean %.1f", last.Mean)})
		return rep, nil
	}
	emit(Event{Round: 0, Stage: "baseline", Detail: "running baseline eval"})
	rep, err := evals.Run(ctx, cl, s, onEval)
	if err != nil {
		return nil, err
	}
	if err := evals.Persist(s.Name, rep); err != nil {
		return nil, err
	}
	return rep, nil
}

func transcript(rep *evals.Report) string {
	out := ""
	for i, f := range rep.Fixtures {
		out += fmt.Sprintf("--- fixture %d (score %.0f) ---\ninput: %s\nexpect: %s\njudge: %s\noutput:\n%s\n\n",
			i+1, f.Score, f.Input, f.Expect, f.Reason, truncate(f.Output, 2000))
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n[truncated]"
}
