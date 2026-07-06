// Package runner executes an agent once: its system prompt and tier model
// against one task input, streaming the answer.
package runner

import (
	"context"

	"github.com/tamnd/tamago/pkg/llm"
	"github.com/tamnd/tamago/pkg/spec"
)

// Run executes the agent on one input and returns the full output.
// onDelta streams chunks as they arrive; pass nil to collect silently.
func Run(ctx context.Context, cl *llm.Client, s *spec.AgentSpec, input string, onDelta func(string)) (string, error) {
	return cl.StreamText(ctx, s.Model(), s.SystemPrompt, input, 16000, onDelta)
}
