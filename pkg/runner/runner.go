// Package runner executes an agent once. Agents without tools are a single
// streamed completion. Agents with tools run an action loop: the model
// requests one tool call per turn as plain JSON, tamago executes it behind
// the risk and approval gates, and the observation goes back into the
// transcript. The protocol is plain text, so it works on any wire, including
// OpenAI-compatible servers with no native tool-call support.
package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tamnd/tamago/pkg/llm"
	"github.com/tamnd/tamago/pkg/spec"
	"github.com/tamnd/tamago/pkg/tools"
)

// DefaultMaxSteps bounds the action loop.
const DefaultMaxSteps = 8

// Step is one executed (or denied) tool call, for progress display and records.
type Step struct {
	N           int
	Tool        string
	Args        tools.Args
	Observation string
	Denied      bool
	Err         error
}

// Options tune a tool-agent run.
type Options struct {
	MaxSteps int
	// Approve is asked before any write- or admin-floor tool executes.
	// Read-floor tools never need approval. nil denies write and admin.
	Approve func(name string, a tools.Args) bool
	// OnStep sees every tool call after it resolves.
	OnStep func(Step)
}

// Run executes the agent with default options: read tools allowed, write and
// admin tools denied. onDelta streams the answer for tool-less agents.
func Run(ctx context.Context, cl llm.Client, s *spec.AgentSpec, input string, onDelta func(string)) (string, error) {
	return RunWithOptions(ctx, cl, s, input, onDelta, Options{})
}

// RunWithOptions executes the agent with explicit approval and step control.
func RunWithOptions(ctx context.Context, cl llm.Client, s *spec.AgentSpec, input string, onDelta func(string), opts Options) (string, error) {
	model := cl.ModelForTier(s.Tier)
	if len(s.Tools) == 0 {
		return cl.StreamText(ctx, model, s.SystemPrompt, input, 16000, onDelta)
	}
	if opts.MaxSteps <= 0 {
		opts.MaxSteps = DefaultMaxSteps
	}
	system := s.SystemPrompt + "\n\n" + actionInstructions(s)
	var transcript strings.Builder
	fmt.Fprintf(&transcript, "Task:\n%s\n", input)

	for step := 1; step <= opts.MaxSteps; step++ {
		reply, err := cl.StreamText(ctx, model, system, transcript.String(), 16000, nil)
		if err != nil {
			return "", err
		}
		act, ok := parseAction(reply)
		if !ok || act.Final != "" || act.Tool == "" {
			if act.Final != "" {
				reply = act.Final
			}
			if onDelta != nil {
				onDelta(reply)
			}
			return reply, nil
		}
		obs, denied := execute(ctx, s, act, opts)
		st := Step{N: step, Tool: act.Tool, Args: act.args, Observation: obs, Denied: denied}
		if opts.OnStep != nil {
			opts.OnStep(st)
		}
		fmt.Fprintf(&transcript, "\nAction %d: %s %s\nObservation %d:\n%s\n", step, act.Tool, marshalArgs(act.args), step, obs)
	}

	// Out of steps: one last turn that may only answer.
	transcript.WriteString("\nYou have used all your tool calls. Reply now with {\"final\": \"...\"} only.\n")
	reply, err := cl.StreamText(ctx, cl.ModelForTier(s.Tier), system, transcript.String(), 16000, nil)
	if err != nil {
		return "", err
	}
	if act, ok := parseAction(reply); ok && act.Final != "" {
		reply = act.Final
	}
	if onDelta != nil {
		onDelta(reply)
	}
	return reply, nil
}

// action is what the model replies each turn.
type action struct {
	Tool  string         `json:"tool"`
	Args  map[string]any `json:"args"`
	Final string         `json:"final"`
	// parsed string args
	args tools.Args
}

func parseAction(reply string) (action, bool) {
	raw, err := llm.ExtractJSON(reply)
	if err != nil {
		return action{}, false
	}
	var a action
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return action{}, false
	}
	a.args = tools.Args{}
	for k, v := range a.Args {
		a.args[k] = fmt.Sprint(v)
	}
	return a, true
}

func execute(ctx context.Context, s *spec.AgentSpec, act action, opts Options) (obs string, denied bool) {
	if tools.Floor(act.Tool) != spec.RiskRead {
		if opts.Approve == nil || !opts.Approve(act.Tool, act.args) {
			return fmt.Sprintf("tool %s was denied by the operator; continue without it or finish", act.Tool), true
		}
	}
	out, err := tools.Run(ctx, s, act.Tool, act.args)
	if err != nil {
		return "error: " + err.Error(), false
	}
	return out, false
}

func marshalArgs(a tools.Args) string {
	b, _ := json.Marshal(a)
	return string(b)
}

func actionInstructions(s *spec.AgentSpec) string {
	var sb strings.Builder
	sb.WriteString("You can use tools. Available tools and their arguments:\n")
	for _, t := range s.Tools {
		help, ok := tools.Help[t]
		if !ok {
			help = "(not configured on this machine)"
		}
		fmt.Fprintf(&sb, "- %s %s\n", t, help)
	}
	sb.WriteString(`
To use a tool, reply with ONLY this JSON, nothing else:
{"tool": "name", "args": {...}}
One tool call per reply. The result comes back as an observation, then you decide the next step.
When you have the answer, reply with ONLY:
{"final": "your complete answer"}
If the task needs no tools, reply with the final JSON immediately.`)
	return sb.String()
}
