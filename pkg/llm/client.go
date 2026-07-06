// Package llm wraps the Anthropic SDK for tamago's own calls: the designer
// that writes agents, the judge that scores them, and the rewriter that grows
// them. Generated agents get their own client in generated code.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// DesignerModel runs tamago's design, judge, and rewrite calls.
const DesignerModel = anthropic.ModelClaudeOpus4_8

// Client is a thin wrapper over the Anthropic client.
type Client struct {
	c anthropic.Client
}

// New builds a client from the environment (ANTHROPIC_API_KEY et al).
func New() *Client {
	return &Client{c: anthropic.NewClient()}
}

func adaptive() anthropic.ThinkingConfigParamUnion {
	return anthropic.ThinkingConfigParamUnion{OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{}}
}

// StreamText streams a completion and returns the full text. onDelta gets
// each text chunk as it arrives; pass nil to collect silently.
func (cl *Client) StreamText(ctx context.Context, model, system, user string, maxTokens int64, onDelta func(string)) (string, error) {
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
	stream := cl.c.Messages.NewStreaming(ctx, params)
	var sb strings.Builder
	for stream.Next() {
		event := stream.Current()
		switch ev := event.AsAny().(type) {
		case anthropic.ContentBlockDeltaEvent:
			if d, ok := ev.Delta.AsAny().(anthropic.TextDelta); ok {
				sb.WriteString(d.Text)
				if onDelta != nil {
					onDelta(d.Text)
				}
			}
		}
	}
	if err := stream.Err(); err != nil {
		return "", wrapErr(err)
	}
	return sb.String(), nil
}

// JSON streams a structured-output request constrained by schema and
// unmarshals the result into out. onDelta gets raw JSON chunks for progress.
func (cl *Client) JSON(ctx context.Context, model, system, user string, maxTokens int64, schema map[string]any, out any, onDelta func(string)) error {
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
	text, err := func() (string, error) {
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
		return sb.String(), stream.Err()
	}()
	if err != nil {
		return wrapErr(err)
	}
	if err := json.Unmarshal([]byte(text), out); err != nil {
		return fmt.Errorf("model returned invalid JSON: %w", err)
	}
	return nil
}

// wrapErr turns SDK errors into something actionable at the CLI.
func wrapErr(err error) error {
	var apierr *anthropic.Error
	if errors.As(err, &apierr) {
		switch apierr.StatusCode {
		case 401:
			return fmt.Errorf("authentication failed: set ANTHROPIC_API_KEY (or run `ant auth login`): %w", err)
		case 429:
			return fmt.Errorf("rate limited, retry in a moment: %w", err)
		case 529:
			return fmt.Errorf("API overloaded, retry in a moment: %w", err)
		}
	}
	return err
}
