// Package evals runs an agent against its fixture suite and scores each
// output with an LLM judge.
package evals

import (
	"context"
	"fmt"
	"time"

	"github.com/tamnd/tamago/pkg/garden"
	"github.com/tamnd/tamago/pkg/llm"
	"github.com/tamnd/tamago/pkg/runner"
	"github.com/tamnd/tamago/pkg/spec"
)

// PassThreshold is the mean score an agent needs to pass its suite.
const PassThreshold = 70.0

const judgeSystem = `You judge AI agent outputs. You get the agent's job, one task input,
the criteria the output must meet, and the output. Score 0-100:
90+ meets every criterion cleanly, 70-89 meets the substance with rough edges,
40-69 partially meets it, below 40 misses the point or is unsafe.
Be strict about the stated criteria, not about style preferences they do not mention.`

// FixtureResult is one scored fixture.
type FixtureResult struct {
	Input  string  `json:"input"`
	Expect string  `json:"expect"`
	Output string  `json:"output"`
	Score  float64 `json:"score"`
	Reason string  `json:"reason"`
}

// Report is a full eval run.
type Report struct {
	Generation int             `json:"generation"`
	When       string          `json:"when"`
	Mean       float64         `json:"mean"`
	Pass       bool            `json:"pass"`
	Fixtures   []FixtureResult `json:"fixtures"`
}

func judgeSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"score":  map[string]any{"type": "number", "minimum": 0, "maximum": 100},
			"reason": map[string]any{"type": "string", "description": "one or two sentences"},
		},
		"required":             []string{"score", "reason"},
		"additionalProperties": false,
	}
}

// Event reports eval progress: fixture index (1-based), stage, and detail.
type Event struct {
	Fixture int
	Total   int
	Stage   string // "run" or "judge" or "done"
	Detail  string
}

// Run executes the whole suite. onEvent gets progress; pass nil for silence.
func Run(ctx context.Context, cl llm.Client, s *spec.AgentSpec, onEvent func(Event)) (*Report, error) {
	emit := func(e Event) {
		if onEvent != nil {
			onEvent(e)
		}
	}
	rep := &Report{
		Generation: s.Meta.Generation,
		When:       time.Now().UTC().Format(time.RFC3339),
	}
	total := len(s.Evals)
	var sum float64
	for i, f := range s.Evals {
		emit(Event{Fixture: i + 1, Total: total, Stage: "run", Detail: f.Input})
		out, err := runner.Run(ctx, cl, s, f.Input, nil)
		if err != nil {
			return nil, fmt.Errorf("fixture %d run: %w", i+1, err)
		}
		emit(Event{Fixture: i + 1, Total: total, Stage: "judge"})
		user := fmt.Sprintf("Agent job: %s\n\nTask input:\n%s\n\nCriteria:\n%s\n\nAgent output:\n%s",
			s.Job, f.Input, f.Expect, out)
		var verdict struct {
			Score  float64 `json:"score"`
			Reason string  `json:"reason"`
		}
		if err := cl.JSON(ctx, cl.DesignerModel(), judgeSystem, user, 2000, judgeSchema(), &verdict, nil); err != nil {
			return nil, fmt.Errorf("fixture %d judge: %w", i+1, err)
		}
		sum += verdict.Score
		rep.Fixtures = append(rep.Fixtures, FixtureResult{
			Input: f.Input, Expect: f.Expect, Output: out,
			Score: verdict.Score, Reason: verdict.Reason,
		})
		emit(Event{Fixture: i + 1, Total: total, Stage: "done",
			Detail: fmt.Sprintf("%.0f %s", verdict.Score, verdict.Reason)})
	}
	if total > 0 {
		rep.Mean = sum / float64(total)
	}
	rep.Pass = rep.Mean >= PassThreshold
	return rep, nil
}

// Persist stores the report in the garden.
func Persist(name string, rep *Report) error {
	gr := &garden.EvalResult{
		Generation: rep.Generation,
		When:       rep.When,
		Mean:       rep.Mean,
		Pass:       rep.Pass,
	}
	for _, f := range rep.Fixtures {
		gr.Fixtures = append(gr.Fixtures, struct {
			Input  string  `json:"input"`
			Expect string  `json:"expect"`
			Output string  `json:"output"`
			Score  float64 `json:"score"`
			Reason string  `json:"reason"`
		}{f.Input, f.Expect, f.Output, f.Score, f.Reason})
	}
	return garden.SaveEval(name, gr)
}
