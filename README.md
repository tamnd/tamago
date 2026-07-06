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

## Tools at run time

Agents whose spec declares tools actually use them: `tamago run` becomes an action loop where the model requests one tool call per turn as plain JSON, tamago executes it, and the observation goes back into the transcript.
The protocol is plain text, so it works on any wire, including OpenAI-compatible servers with no native tool-call support.

Three gates stand between the model and your machine.
Design time: the risk gate refuses specs whose tools exceed their declared class.
Run time: every call re-checks the allowlist and the tool's risk floor.
Approval: write and admin tools ask on the terminal before executing; `--yes` skips the question for unattended runs, and a denial becomes an observation the agent can react to instead of a crash.

Eval suites run with write and admin tools denied, so they are always safe to run unattended.
Tools that have no real backend on your machine (like `deploy`) fail with an honest not-configured error; nothing is ever faked.

Chat-tuned servers sometimes answer in prose or claim the tools do not exist.
The runner pushes back once, restating that the calls really execute, then accepts what comes; the format demand also rides at the end of every turn where such models actually look.

## Unattended runs

Every `tamago run` writes a record to `garden/NAME/runs/` with the input, each tool call and observation, the final output, and the duration; `tamago show` lists the last five next to the eval history.

Flags for schedulers: `--quiet` drops streaming and step lines, `--out FILE` routes the final output to a file, `--timeout 5m` puts a deadline on the whole run.

Exit codes: 0 ok, 1 error, 2 the agent escalated.
Designed agents are taught to start their answer with `ESCALATE:` when their escalation rule triggers, and tamago turns that into exit 2 so cron can alert a human.

The cron target only renders for read-risk agents; a write or admin agent scheduled with no human watching is a design smell, so `tamago gen` refuses and says to run it manually with `--yes` instead.

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
| `tamago run NAME ["input"]` | run once, input from arg or stdin (`--yes`, `--quiet`, `--out`, `--timeout`, `--max-steps`) |
| `tamago eval NAME` | run the fixture suite, exit 1 below threshold 70 |
| `tamago grow NAME` | improvement rounds (`--rounds`) |
| `tamago ls` | list the garden |
| `tamago show NAME` | spec, lineage, eval history, recent runs |

## License

MIT
