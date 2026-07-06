package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/tamnd/tamago/pkg/spec"
)

// anthropicDesigner runs tamago's design, judge, and rewrite calls.
const anthropicDesigner = string(anthropic.ModelClaudeOpus4_8)

type anthropicClient struct {
	c anthropic.Client
}

func newAnthropic() *anthropicClient {
	return &anthropicClient{c: anthropic.NewClient()}
}

func (cl *anthropicClient) Name() string          { return "anthropic" }
func (cl *anthropicClient) DesignerModel() string { return anthropicDesigner }

func (cl *anthropicClient) ModelForTier(tier string) string {
	if m, ok := spec.TierModel[tier]; ok {
		return m
	}
	return spec.TierModel[spec.TierStandard]
}

func adaptive() anthropic.ThinkingConfigParamUnion {
	return anthropic.ThinkingConfigParamUnion{OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{}}
}

func (cl *anthropicClient) StreamText(ctx context.Context, model, system, user string, maxTokens int64, onDelta func(string)) (string, error) {
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: maxTokens,
		Thinking:  adaptive(),
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(user)),
		},
	}
	if system != "" {
		params.System = []anthropic.TextBlockParam{{Text: system}}
	}
	return cl.collect(ctx, params, onDelta)
}

func (cl *anthropicClient) JSON(ctx context.Context, model, system, user string, maxTokens int64, schema map[string]any, out any, onDelta func(string)) error {
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: maxTokens,
		Thinking:  adaptive(),
		OutputConfig: anthropic.OutputConfigParam{
			Format: anthropic.JSONOutputFormatParam{Schema: schema},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(user)),
		},
	}
	if system != "" {
		params.System = []anthropic.TextBlockParam{{Text: system}}
	}
	text, err := cl.collect(ctx, params, onDelta)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(text), out); err != nil {
		return fmt.Errorf("model returned invalid JSON: %w", err)
	}
	return nil
}

func (cl *anthropicClient) collect(ctx context.Context, params anthropic.MessageNewParams, onDelta func(string)) (string, error) {
	stream := cl.c.Messages.NewStreaming(ctx, params)
	var sb strings.Builder
	for stream.Next() {
		event := stream.Current()
		if ev, ok := event.AsAny().(anthropic.ContentBlockDeltaEvent); ok {
			if d, ok := ev.Delta.AsAny().(anthropic.TextDelta); ok {
				sb.WriteString(d.Text)
				if onDelta != nil {
					onDelta(d.Text)
				}
			}
		}
	}
	if err := stream.Err(); err != nil {
		return "", wrapAnthropicErr(err)
	}
	return sb.String(), nil
}

// wrapAnthropicErr turns SDK errors into something actionable at the CLI.
func wrapAnthropicErr(err error) error {
	var apierr *anthropic.Error
	if errors.As(err, &apierr) {
		switch apierr.StatusCode {
		case 401:
			return fmt.Errorf("authentication failed: set ANTHROPIC_API_KEY, or point tamago at an OpenAI-compatible server with OPENAI_API_KEY and OPENAI_BASE_URL: %w", err)
		case 429:
			return fmt.Errorf("rate limited, retry in a moment: %w", err)
		case 529:
			return fmt.Errorf("API overloaded, retry in a moment: %w", err)
		}
	}
	return err
}
