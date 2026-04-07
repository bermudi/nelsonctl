# AGENTS.md

## What This Is

nelsonctl is a harness that automates the execution phase of litespec's spec-driven development workflow. It is not a general-purpose tool — it exists specifically to drive agents through litespec's apply → review → fix loop.

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

## Key Design Decisions

- **Prompts reference litespec skills explicitly.** This is a litespec harness. The agent must know to use `litespec-apply` and `litespec-review` skills — the prompts tell it to.
- **Artifact paths are embedded in prompts.** The agent gets `specs/changes/<name>/proposal.md` and `specs/changes/<name>/tasks.md` so there is no ambiguity about where to find context.
- **One change per run.** Not multi-repo, not multi-change.
- **Agents are pluggable.** The current scaffold shells out to agent CLIs (`opencode`, `claude`, `codex`, `amp`). Two upcoming changes replace this with direct RPC: **pi-rpc-integration** adds a long-lived Pi adapter (JSONL-RPC over stdio) and the **opencode-rpc-integration** adds an opencode adapter (HTTP to `opencode serve`). Both implement a shared `RPCAgent` interface with persistent sessions, per-step model selection, SSE streaming, and crash recovery. CLI adapters remain as a dumb-path fallback.
- **No interactive prompting during execution.** This is a pipeline, not a chat app.
- **Review detection is evolving.** Today it parses agent stdout for pass/fail markers ("pass", "lgtm", "fail", "rejected") with heuristic substring fallback. pi-rpc-integration replaces this with an AI controller.

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
