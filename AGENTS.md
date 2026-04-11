# AGENTS.md

## What This Is

nelsonctl is a harness that automates the execution phase of litespec's spec-driven development workflow. It is not a general-purpose tool — it exists specifically to drive agents through litespec's apply → review → fix loop.

## Two AI Roles

There are two distinct AI systems in nelsonctl. They serve completely different purposes and are configured independently:

1. **The Agent** — the coding tool (opencode, claude, codex, amp, pi) that actually writes, edits, and reviews code. Configured in `steps.apply` and `steps.review` in the nelsonctl config. Each step can use a different model. The agent is the "worker" — it receives prompts, uses litespec skills, and produces code changes or review output.

2. **The Controller** — nelsonctl's own AI brain that evaluates review results, decides pass/fail, and orchestrates the pipeline state machine. Configured in `controller` in the nelsonctl config (`provider`, `model`, `max_tool_calls`, `timeout`). The controller reads the agent's review output and makes the judgment call: is this phase done, or does the agent need to try again? It lives in `internal/controller/` and is completely separate from the agent adapters in `internal/agent/`.

In short: **the agent builds the code, the controller judges it.** Confusingly, both are LLM-backed, but they answer different questions. The agent answers "what changes satisfy this spec?" The controller answers "did the agent's review output indicate success or failure?"

## The Workflow

litespec has two halves:

1. **Thinking phase** (interactive, human + agent): explore → grill → propose → review. Produces `proposal.md` and `tasks.md` in `specs/changes/<name>/`.
2. **Execution phase** (automated, nelsonctl): nelsonctl takes over and drives the agent through each phase in `tasks.md`, reviewing and fixing until everything passes, then commits and opens a PR.

The human hands off to nelsonctl after the thinking phase is done. Nelsonctl runs the dumb loop — it does not make decisions about what to build. The specs are the contract.

## How It Works

The pipeline state machine: `Init → Branch → CommitArtifacts → PhaseLoop → FinalReview → PR → Done`

For each phase:
1. **Apply** — prompts the agent with `"Use your litespec-apply skill to implement phase N..."` including paths to `proposal.md` and `tasks.md`
2. **Review** — prompts the agent with `"Use your litespec-review skill to review..."` against the proposal
3. **Fix** — if review fails, feeds the review output back and retries (max 3 attempts per phase)
4. **Commit** — on success, commits with `feat(<change>): complete phase N - <name>`

After all phases pass, a final pre-archive review runs, then the branch is pushed and a PR is opened via `gh`.

## Pi Source Code — Read-Only Reference

Pi's source code lives at `~/build/pi-mono/packages/coding-agent`, but **Pi was installed via `pnpm`** — the running binary comes from the pnpm store, not that directory. **Do not edit files under `~/build/pi-mono/packages/coding-agent`** expecting changes to take effect; they won't. That tree is a read-only reference for understanding Pi's internals, not a live installation. If you find a bug or limitation in Pi, **document it as an upstream issue** (in AGENTS.md, a GitHub issue, or similar) rather than patching the local source. Patches there are invisible to the running process and will only cause confusion.

## Key Design Decisions

- **Prompts reference litespec skills explicitly.** This is a litespec harness. The agent must know to use `litespec-apply` and `litespec-review` skills — the prompts tell it to.
- **Artifact paths are embedded in prompts.** The agent gets `specs/changes/<name>/proposal.md` and `specs/changes/<name>/tasks.md` so there is no ambiguity about where to find context.
- **One change per run.** Not multi-repo, not multi-change.
- **Agents are pluggable.** The current scaffold shells out to agent CLIs (`opencode`, `claude`, `codex`, `amp`). Two upcoming changes replace this with direct RPC: **pi-rpc-integration** adds a long-lived Pi adapter (JSONL-RPC over stdio) and the **opencode-rpc-integration** adds an opencode adapter (HTTP to `opencode serve`). Both implement a shared `RPCAgent` interface with persistent sessions, per-step model selection, SSE streaming, and crash recovery. CLI adapters remain as a dumb-path fallback.
- **No interactive prompting during execution.** This is a pipeline, not a chat app.
- **The controller replaced heuristic review detection.** Early versions parsed agent stdout for pass/fail markers ("pass", "lgtm", "fail", "rejected") with heuristic substring fallback. The controller (`internal/controller/`) now uses an LLM call to evaluate review output instead.

## Project Structure

```
cmd/nelsonctl/          CLI entry point, flag parsing, verbose mode
internal/
  agent/                Agent interface + adapters (opencode, claude, codex, amp)
  git/                  Git operations (branch, add, commit, push)
  pipeline/             State machine, prompt construction, review detection, retry loop
  trace/                Trace file writer for run auditing
  tui/                  Bubble Tea TUI (two-panel: progress + output)
  version/              Version info
specs/changes/          Litespec change directories (proposal, tasks, per-component specs)
```
