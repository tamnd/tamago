// Package llm wraps the model providers behind one interface for tamago's
// own calls: the designer that writes agents, the judge that scores them,
// and the rewriter that grows them.
//
// Two providers exist: the Anthropic API (default) and any OpenAI-compatible
// server. Selection order: TAMAGO_PROVIDER (anthropic or openai) wins, then
// ANTHROPIC_API_KEY picks anthropic, then OPENAI_API_KEY picks openai, and
// with nothing set anthropic is used so its auth error explains what to do.
package llm

import (
	"context"
	"os"
)

// Client is one model provider.
type Client interface {
	// Name identifies the provider: anthropic or openai.
	Name() string
	// DesignerModel is the model used for design, judge, and rewrite calls.
	DesignerModel() string
	// ModelForTier maps an agent tier (fast, standard, deep) to a model ID.
	ModelForTier(tier string) string
	// StreamText streams a completion and returns the full text. onDelta gets
	// each chunk as it arrives; pass nil to collect silently.
	StreamText(ctx context.Context, model, system, user string, maxTokens int64, onDelta func(string)) (string, error)
	// JSON requests output constrained to schema and unmarshals it into out.
	JSON(ctx context.Context, model, system, user string, maxTokens int64, schema map[string]any, out any, onDelta func(string)) error
}

// New picks a provider from the environment.
func New() Client {
	switch os.Getenv("TAMAGO_PROVIDER") {
	case "openai":
		return newOpenAI()
	case "anthropic":
		return newAnthropic()
	}
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		return newAnthropic()
	}
	if os.Getenv("OPENAI_API_KEY") != "" {
		return newOpenAI()
	}
	return newAnthropic()
}
