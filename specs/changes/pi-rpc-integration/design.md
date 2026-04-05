# pi-rpc-integration — Design Document

This change upgrades the `initial-scaffold` architecture from a pure shell-out loop to a Pi-first execution model while preserving the CLI adapters as an explicit fallback path. The shape of the product stays the same: nelsonctl is still a pipeline runner over litespec artifacts, not a chat interface and not a general model host.

## Architecture

The runtime splits into four cooperating layers:

1. Configuration and startup validation resolve the effective agent, per-step models, timeouts, controller settings, workspace prerequisites, and lock state before any agent work begins.
2. The pipeline orchestrator stays in charge of branch setup, resume detection, phase progression, review gates, commits, and PR creation.
3. The agent layer now has two execution modes:
   - CLI adapters for the original one-command-per-step loop.
   - A Pi RPC adapter that keeps one implementation session alive for apply and fix, while spinning up a fresh review session for each review pass.
4. The review layer normalizes raw reviewer output into a structured result that the pipeline can trust for retry decisions.

In Nelson mode, startup resolves Pi plus controller-backed review parsing. The pipeline creates one Pi process for the run, starts an apply session, and reuses that session for every fix step. Each review pass creates a disposable review session so the reviewer does not inherit implementation bias. The review parser attempts fuzzy structured parsing first, falls back to heuristics second, and only then asks the controller summarizer to compress ambiguous output into machine-usable issues.

In Ralph mode, the pipeline skips RPC entirely and keeps the scaffold's shell-out behavior. This preserves a zero-config fallback path and avoids forcing API keys or Pi on users who explicitly want a plain CLI agent.

The TUI remains a passive consumer. Pi output events are coalesced before they are forwarded to Bubble Tea, but the pipeline never buffers terminal events such as message end, retry, crash, or completion.

## Decisions

### Two-tier adapter interface

The agent layer should expose one pipeline-facing contract with an optional RPC capability rather than two unrelated code paths. That lets the pipeline make one runtime type decision and keep phase logic, prompts, retry policy, and commit behavior centralized.

### Pi-first, explicit CLI fallback

`pi` becomes the preferred agent because it is the only path that can preserve implementation context, switch models between steps, and recover from crashes without losing the on-disk work. CLI agents stay available, but only when the user explicitly chooses them, so nelsonctl does not silently degrade into a different agent than the one the operator intended.

### Shared implementation session, disposable review sessions

Apply and fix share context because they are two parts of the same implementation loop. Review uses a fresh session every time because the goal is independent assessment. This keeps the implementation prompt compact while preventing the reviewer from inheriting the apply session's assumptions.

### Three-tier review extraction

The review parser should prefer deterministic extraction over model calls. The first pass handles the expected litespec-review shape, the second catches sloppy or partial output, and the controller is reserved for ambiguous cases. This keeps cost low, makes failures explainable, and still gives the pipeline a way out when the reviewer does not follow the contract closely enough.

### Config as durable state, environment for credentials

The durable operator choices belong in `~/.config/nelsonctl/config.yaml`: agent, per-step models, timeouts, controller, and review policy. Credentials stay in environment variables so the config file is safe to write from `nelsonctl init`. If environment overrides for provider or model are supported, they should be treated as ephemeral process overrides rather than the primary source of truth.

### Crash recovery from disk, not transcript restore

If Pi crashes mid-phase, nelsonctl restarts Pi and re-prompts the current phase from scratch. The files are the source of truth, not the prior chat transcript. This is the simplest design that preserves work, matches the decision recorded in `BUGS.md`, and avoids introducing session-restore complexity for a rare edge case.

### Resume from checked tasks

The pipeline should use `tasks.md` as the durable progress marker. On resume, it finds the first unchecked task, preserves any leftover tracked changes with a recovery commit, and continues from that point. This is more robust than keeping extra progress metadata and fits the litespec workflow that already centers the task checklist.

### Scoped staging and locking

Staging only files reported by `git diff --name-only` keeps nelsonctl from accidentally committing unrelated workspace changes. A lock file in the change directory prevents overlapping runs against the same change and gives crash recovery a clear stale-lock cleanup path.

## File Changes

- `cmd/nelsonctl/main.go` — load config, resolve the effective mode, wire the `init` subcommand, and route run vs setup behavior. Supports `Config File`, `Initialization Wizard`, and `Pi-First Agent Resolution`.
- `cmd/nelsonctl/init.go` — interactive command entrypoint for first-run setup. Supports `Initialization Wizard`.
- `internal/config/config.go` — define config structs, defaults, XDG loading, validation, and review-threshold parsing. Supports `Config File` and `Review Failure Threshold`.
- `internal/config/wizard.go` — implement the minimal and advanced setup flows that write `~/.config/nelsonctl/config.yaml` without secrets. Supports `Initialization Wizard` and `Credential Handling`.
- `internal/agent/adapter.go` — redefine the pipeline-facing agent contract so CLI and RPC implementations can share one orchestration layer. Supports `Two-Tier Agent Execution`, `Pi-First Agent Resolution`, and `Step Timeouts and Abort`.
- `internal/agent/pi.go` — manage Pi process lifecycle, session creation, model switching, skill discovery, extension cancellation, and crash restart hooks. Supports `Pi RPC Sessions` and `Pi Crash Recovery`.
- `internal/agent/rpcclient.go` — implement JSONL framing, request IDs, request-response correlation, and event fan-out for Pi stdio RPC. Supports `Pi RPC Sessions` and `Streaming Agent Output`.
- `internal/agent/rpctypes.go` — declare typed request, response, and event payloads for Pi RPC. Supports `Pi RPC Sessions`.
- `internal/agent/opencode.go`, `internal/agent/claude.go`, `internal/agent/codex.go`, `internal/agent/amp.go` — update the CLI adapters to satisfy the new contract while preserving the explicit fallback path. Supports `Two-Tier Agent Execution` and `Agent Prerequisite Check`.
- `internal/pipeline/pipeline.go` — implement mode selection, branch reuse, resume logic, lock acquisition, step execution, and dry-run planning. Supports `Resumable Change Branch`, `Pi-Aware Phase Execution`, `Run Locking`, `Workspace Validation`, and `Dry-Run Plan`.
- `internal/pipeline/phase.go` — extend task parsing so the orchestrator can resume from checked tasks and assemble phase-local task lists cleanly. Supports `Pi-Aware Phase Execution`.
- `internal/pipeline/prompt.go` — build apply, review, fix, and steer prompts, including compact parsed-issue payloads for Pi fix steps. Supports `Independent Review Sessions` and `Parsed Retry Loop`.
- `internal/review/parser.go` — fuzzy parser for severity headers, issue blocks, and scorecard extraction. Supports `Three-Tier Review Detection`.
- `internal/review/heuristics.go` — fallback pattern matcher for incomplete reviewer output. Supports `Three-Tier Review Detection`.
- `internal/review/summarizer.go` — raw HTTP controller clients for OpenAI, Anthropic, and OpenRouter. Supports `Controller Summarizer` and `Credential Handling`.
- `internal/git/git.go` — implement branch reuse checks, recovery commits, scoped staging, and the unchanged PR flow. Supports `Resumable Change Branch` and `Recovery Commits and Scoped Staging`.
- `internal/tui/model.go`, `internal/tui/update.go`, `internal/tui/view.go` — surface mode, active model, resume state, Pi restart events, and coalesced output in the existing two-panel UI. Supports `Execution Context Visibility`, `Pi Event Rendering`, and `Exit Summary Includes Mode`.
- `README.md` — document Pi-first setup, `nelsonctl init`, configuration, environment variables, and explicit CLI fallback. Supports `Config File` and `Initialization Wizard`.
