# tamago

Agents that write agents.

Give tamago one line about a job and it hatches a complete agent: a role, a production system prompt, a tool allowlist inside a declared risk budget, an escalation rule, and an eval suite it must pass.
Then it can generate that agent for real runtimes, run it, score it, and grow it generation by generation.

tamago means egg (卵). Agents hatch agents.

## How it works

```
tamago new "summarize merged PRs into a weekly changelog"
```

An LLM designer (Claude Opus 4.8) turns the job into an AgentSpec: name, role, system prompt, tools, risk class, model tier, triggers, and 3 to 5 eval fixtures born with the agent.
A risk gate refuses any design whose tools exceed its declared class, so a read-only agent can never come out holding `git_push`.

The spec lands in the garden at `~/.tamago/garden/NAME/` with its full generation history.

```
tamago eval changelog-scribe     # run the fixtures, an LLM judge scores 0-100 each
tamago gen changelog-scribe      # emit runnable artifacts
tamago run changelog-scribe "..."
tamago grow changelog-scribe     # eval, rewrite, keep the better generation
```

`grow` reruns the suite, feeds the failing transcripts to a rewriter, saves the candidate as generation N+1, re-evals, and keeps it only when the mean score actually improves.

## TUI

Bare `tamago` opens the dashboard: the garden on the left, the selected agent on the right, live streaming output for hatch, run, eval, and grow.

Keys: `j/k` move, `enter` spec, `n` new, `r` run, `e` eval, `g` grow, `esc` cancel, `q` quit.

## Codegen targets

| target | output |
|---|---|
| `claude` | `.claude/agents/NAME.md` Claude Code subagent, tools and model mapped from the spec |
| `standalone` | a single-file Go program on the Anthropic SDK, ready for `go run` |
| `cron` | shell wrapper plus crontab line, when the spec has a cron trigger |

Generation is deterministic: no LLM at gen time, the same spec always produces the same files.

## The spec

```yaml
name: changelog-scribe
job: summarize merged PRs into a weekly changelog
role: a careful release-notes writer ...
risk: read          # read | write | admin, gates the tool allowlist
tier: standard      # fast | standard | deep -> haiku | sonnet | opus
system_prompt: |
  ...
tools: [web_fetch, read_file]
triggers:
  - type: cron
    cron: "0 9 * * MON"
escalation: when a PR looks like a security fix
evals:
  - input: three PRs merged, one breaking
    expect: groups by area and flags the break prominently
meta:
  generation: 3
  parent: release-notes-bot   # lineage, from `tamago new --from`
```

## Install

```
go install github.com/tamnd/tamago@latest
export ANTHROPIC_API_KEY=...
```

The designer, judge, and rewriter all run on the real API from the first command; there is no offline or mock mode.

## Providers

tamago speaks two wires: the Anthropic API and any OpenAI-compatible chat completions server.

Selection order: `TAMAGO_PROVIDER` (`anthropic` or `openai`) wins, else `ANTHROPIC_API_KEY` picks Anthropic, else `OPENAI_API_KEY` picks OpenAI-compatible, else Anthropic.

```
export TAMAGO_PROVIDER=openai
export OPENAI_BASE_URL=http://localhost:8080/v1   # default https://api.openai.com/v1
export OPENAI_API_KEY=...
```

| variable | default | role |
|---|---|---|
| `TAMAGO_OPENAI_MODEL` | `gpt-5` | designer, judge, rewriter |
| `TAMAGO_MODEL_FAST` | `gpt-5-mini` | tier `fast` |
| `TAMAGO_MODEL_STANDARD` | `gpt-5` | tier `standard` |
| `TAMAGO_MODEL_DEEP` | `gpt-5` | tier `deep` |

Many OpenAI-compatible servers ignore `response_format`, so tamago never relies on it: JSON answers are requested by embedding the schema in the prompt, extracted tolerantly from chatty replies, and retried once with the parse error when the first attempt is off.
Transient 429 and 5xx responses are retried with a short backoff before any output has streamed.

## Commands

| command | what it does |
|---|---|
| `tamago` | open the TUI |
| `tamago new "job"` | design and hatch an agent (`--name`, `--from parent`) |
| `tamago gen NAME` | generate artifacts (`--target`, `--out`) |
| `tamago run NAME ["input"]` | run once, input from arg or stdin |
| `tamago eval NAME` | run the fixture suite, exit 1 below threshold 70 |
| `tamago grow NAME` | improvement rounds (`--rounds`) |
| `tamago ls` | list the garden |
| `tamago show NAME` | spec, lineage, eval history |

## License

MIT
