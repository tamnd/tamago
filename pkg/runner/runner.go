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

	// The cue rides at the end of every turn: chat-tuned servers weigh the
	// last line far more than the system text, and some fold the system
	// message into pasted context where it loses authority. The remaining
	// budget stops models from finalizing early out of step anxiety.
	nudged := false
	toolCalls := 0
	for step := 1; step <= opts.MaxSteps; step++ {
		cue := fmt.Sprintf("\nReply now with exactly one JSON object, starting with {, and nothing else. You have %d tool calls left.", opts.MaxSteps-step+1)
		reply, err := cl.StreamText(ctx, model, system, transcript.String()+cue, 16000, nil)
		if err != nil {
			return "", err
		}
		act, ok := parseAction(reply)
		if !ok || (act.Final == "" && act.Tool == "") {
			// Some models answer in prose instead of the action JSON.
			// One correction turn usually gets them back on protocol; a
			// second prose reply is accepted as the answer.
			if !nudged {
				nudged = true
				transcript.WriteString("\nThat reply was not the action JSON. The tools are real: tamago runs them on this machine and pastes the result back to you. Reply with ONLY one JSON object, {\"tool\": ...} or {\"final\": ...}.\n")
				continue
			}
			if onDelta != nil {
				onDelta(reply)
			}
			return reply, nil
		}
		if act.Final != "" {
			// Some models finalize immediately, claiming the tools do not
			// exist. Push back once when a tool agent finishes with zero
			// calls; a repeated final is then accepted as given.
			if toolCalls == 0 && !nudged {
				nudged = true
				fmt.Fprintf(&transcript, "\nYou finished without using any tool. The tools are real: tamago runs them on this machine, where the data lives, and pastes the output back to you. If the task truly needs no tool, repeat your final answer; otherwise start with a tool call now.\n")
				continue
			}
			reply = act.Final
			if act.Escalate && !Escalated(reply) {
				reply = EscalationPrefix + " " + reply
			}
			if onDelta != nil {
				onDelta(reply)
			}
			return reply, nil
		}
		toolCalls++
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
		if act.Escalate && !Escalated(reply) {
			reply = EscalationPrefix + " " + reply
		}
	}
	if onDelta != nil {
		onDelta(reply)
	}
	return reply, nil
}

// EscalationPrefix marks a final answer that must reach a human. Agents are
// told to start their reply with it when their escalation rule triggers, and
// schedulers get exit code 2 so they can alert.
const EscalationPrefix = "ESCALATE:"

// Escalated reports whether a final answer declares an escalation.
func Escalated(out string) bool {
	return strings.HasPrefix(strings.TrimSpace(out), EscalationPrefix)
}

// action is what the model replies each turn.
type action struct {
	Tool     string         `json:"tool"`
	Args     map[string]any `json:"args"`
	Final    string         `json:"final"`
	Escalate bool           `json:"escalate"`
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
	sb.WriteString("You run inside an automated pipeline. A program parses every reply you send; no human reads them. You can use tools:\n")
	for _, t := range s.Tools {
		help, ok := tools.Help[t]
		if !ok {
			help = "(not configured on this machine)"
		}
		fmt.Fprintf(&sb, "- %s %s\n", t, help)
	}
	sb.WriteString(`
The program runs each tool call on the machine where the data lives and pastes the output back to you as an observation. Never say the tools are unavailable.
Reply format, hard rule: your reply must start with the character { and contain exactly one JSON object.
{"tool": "name", "args": {...}} to call a tool. One call per reply; the observation comes back, then you decide the next step.
{"final": "your complete answer"} when done. If the task needs no tools, send it immediately.
If your escalation rule triggers, add "escalate": true to the final JSON.
Never greet. Never explain outside the JSON.`)
	return sb.String()
}
