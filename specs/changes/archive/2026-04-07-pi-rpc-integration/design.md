# pi-rpc-integration — Design Document

This change upgrades the `initial-scaffold` architecture from a dumb loop with regex review detection to a controller-driven execution model. The controller AI reasons about the implementation loop — drafting prompts, analyzing reviews, crafting fixes — while the pipeline handles mechanical operations (git, sessions, locks, resume). A Pi RPC adapter adds persistent implementation sessions, but the controller works with both Pi and CLI agents.

## Architecture

The runtime splits into five cooperating layers:

1. **Configuration and startup** resolve the effective agent, controller provider/model, per-step models, timeouts, `review.fail_on` threshold, workspace prerequisites, and lock state before any work begins.

2. **The pipeline orchestrator** handles branch setup, resume detection, phase progression, commits, locking, and PR creation. It is a mechanical state machine that defers all reasoning to the controller.

3. **The controller** drives each phase as a tool-calling agent loop. It receives the phase tasks and `review.fail_on` threshold in its system prompt, discovers specs and code via `read_file`, sends prompts to the agent via `submit_prompt`, triggers reviews via `run_review`, inspects changes via `get_diff`, and ends the phase via `approve`. One controller conversation per phase, discarded after the phase completes. Cross-phase memory is the committed code on disk.

4. **The agent layer** has two execution modes:
   - CLI adapters for the original one-command-per-step loop.
   - A Pi RPC adapter that keeps one implementation session alive for apply and fix, while spinning up a fresh review session for each review pass.
   The controller does not know or care which agent mode is active. It calls `submit_prompt` and gets results back.

5. **The TUI** renders agent output in real time and surfaces controller activity as mechanical status lines derived from tool calls. Full controller reasoning goes to the trace log.

### Controller Loop Flow

For each phase, the pipeline starts a fresh controller conversation:

```
Pipeline starts phase N
  │
  ├─ Creates controller conversation with system prompt:
  │    - Role: implementation controller
  │    - Phase tasks (inlined)
  │    - review.fail_on threshold
  │    - Available tools
  │
  ├─ Controller calls read_file() to discover specs, design
  ├─ Controller calls submit_prompt(rich apply prompt)
  │    ├─ Pipeline routes to agent (Pi RPC or CLI)
  │    ├─ Returns: agent completed (no transcript, just status)
  │
  ├─ Controller calls run_review()
  │    ├─ Pipeline sends mechanical review prompt to agent
  │    ├─ Returns: raw review output
  │
  ├─ Controller reasons about review output...
  │    ├─ Passed? → calls approve(summary) → pipeline commits
  │    └─ Failed? → calls submit_prompt(targeted fix prompt)
  │         ├─ Pipeline injects "Attempt 2 of 3"
  │         ├─ Controller calls run_review() again
  │         └─ Loop continues...
  │
  └─ Pipeline enforces: max 3 attempts, 50 tool calls, 45min timeout
```

The final pre-archive review follows the same pattern: fresh controller conversation, scoped to the full change rather than a single phase.

## Decisions

### Controller as primary brain, not last resort

The controller replaces the three-tier review parser (fuzzy structured parsing → heuristic fallback → controller summarization) with a single AI component that understands review output through comprehension. This kills `ReviewPassed()` regex detection, the heuristic substring matching, and the planned fuzzy parser. One component, fewer failure modes, better results.

### Tool-calling agent loop, not request-response

The controller drives the pipeline by calling tools rather than returning structured responses for the pipeline to interpret. This eliminates output parsing between the controller and the pipeline. The tool call IS the decision — `approve()` means pass, `submit_prompt()` means fix. The pipeline reacts to tool invocations, not parsed text.

### Five constrained tools

`read_file(path)`, `get_diff()`, `submit_prompt(prompt)`, `run_review()`, `approve(summary)`. The controller cannot restart phases, reorder tasks, skip review, or give up. The pipeline enforces the attempt budget mechanically. The controller always wants to keep trying; the pipeline decides when to stop.

### Review prompt stays mechanical

The controller drafts apply and fix prompts but NOT the review prompt. Review uses the mechanical litespec-review prompt so the reviewer operates as a cold read, independent of the controller's theory about what went wrong. This prevents the controller from biasing the reviewer toward looking for or ignoring specific issues.

### One conversation per phase

Each phase gets a fresh controller conversation. Cross-phase memory is the committed code on disk, discoverable via `read_file`. This prevents context window degradation across phases and keeps each phase's reasoning focused on its own tasks.

### Controller is mode-agnostic

The controller works identically in Nelson mode (Pi RPC) and Ralph mode (CLI). It calls `submit_prompt` and gets results. The transport to the implementation agent is invisible to the controller. This means both modes benefit from AI-driven prompt crafting and review analysis.

### review.fail_on as system prompt context

The `review.fail_on` threshold is injected into the controller's system prompt: "Only fail if you find issues at severity X or above." The controller applies it through reasoning, not code. The config is the operator's intent; the controller respects it like a human engineer would.

### DeepSeek 3.2 via direct API or OpenRouter

Both providers use OpenAI-compatible `/chat/completions` with tool calling. One HTTP client implementation, two base URL / auth configurations. The operator can swap models by changing one config line.

### Guardrails

Max 50 tool calls and 45-minute timeout per controller conversation. These are safety nets, not leashes. The pipeline injects attempt count ("Attempt 2 of 3") after each failed review so the controller can adjust strategy on the last attempt.

### Controller failure is resumable

If DeepSeek is unreachable, retry 3× with backoff, then fail the run cleanly. The pipeline is resumable — checked tasks in `tasks.md` mark progress. Relaunch picks up at the failed phase.

### TUI status from tool calls

Controller activity appears as status lines generated mechanically from tool call names: "⚙ Controller: sending apply prompt...", "⚙ Controller: running review...", "⚙ Controller: approved." No extra model needed. The tool call is the structured event.

### Two-tier adapter interface

The agent layer exposes one pipeline-facing contract with an optional RPC capability. The pipeline makes one runtime type decision and keeps phase logic, prompts, retry policy, and commit behavior centralized.

### Pi-first, explicit CLI fallback

`pi` becomes the preferred agent because it preserves implementation context, switches models between steps, and recovers from crashes. CLI agents stay available when the user explicitly chooses them.

### Shared implementation session, disposable review sessions

Apply and fix share context because they are two parts of the same implementation loop. Review uses a fresh session every time for independent assessment.

### Config as durable state, environment for credentials

Operator choices in `~/.config/nelsonctl/config.yaml`. Credentials in environment variables. Config file is safe to version or share.

### Crash recovery from disk, not transcript restore

If Pi crashes mid-phase, nelsonctl restarts Pi and re-prompts the current phase from scratch. Files are source of truth.

### Resume from checked tasks

`tasks.md` is the durable progress marker. On resume, find the first unchecked task, preserve leftover changes with a recovery commit, continue.

### Scoped staging and locking

Stage only files from `git diff --name-only`. Lock file prevents overlapping runs.

## File Changes

- `cmd/nelsonctl/main.go` — load config, resolve effective mode, wire `init` subcommand, route run vs setup.
- `cmd/nelsonctl/init.go` — interactive setup wizard.
- `internal/config/config.go` — config structs, defaults, XDG loading, validation, controller and review-threshold settings.
- `internal/config/wizard.go` — minimal and advanced setup flows.
- `internal/controller/controller.go` — Controller interface, DeepSeek/OpenRouter implementation, tool-calling agent loop, conversation management. This is the brain of the execution phase.
- `internal/controller/tools.go` — Tool definitions (read_file, get_diff, submit_prompt, run_review, approve), argument types, and dispatch logic.
- `internal/controller/prompts.go` — System prompts for phase and final-review controller conversations.
- `internal/agent/adapter.go` — pipeline-facing agent contract with optional RPC capability.
- `internal/agent/pi.go` — Pi process lifecycle, sessions, model switching, crash recovery.
- `internal/agent/rpcclient.go` — JSONL framing, request correlation, event fan-out for Pi stdio RPC.
- `internal/agent/rpctypes.go` — typed RPC payloads.
- `internal/agent/opencode.go`, `claude.go`, `codex.go`, `amp.go` — CLI adapters satisfying the new contract.
- `internal/pipeline/pipeline.go` — state machine calling controller per phase, mechanical git/lock/resume operations.
- `internal/pipeline/phase.go` — task parsing, resume detection from checked tasks.
- `internal/git/git.go` — branch reuse, recovery commits, scoped staging.
- `internal/tui/model.go`, `update.go`, `view.go` — agent output, controller status lines from tool calls, mode/model/resume visibility.
- `README.md` — Pi-first setup, controller configuration, environment variables, CLI fallback.
