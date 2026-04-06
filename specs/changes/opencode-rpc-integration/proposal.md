# opencode-rpc-integration

## Motivation

The current `initial-scaffold` treats opencode as a dumb CLI shell-out: `opencode run --format json` once per step, no persistent sessions, no model switching, no streaming, no abort beyond killing the process. This works but throws away opencode's strongest capability — a full HTTP server with session management, per-message model selection, SSE event streaming, permission handling, and structured output.

Meanwhile, `pi-rpc-integration` plans a Pi-first smart path with persistent sessions, disposable review sessions, model switching, crash recovery, and streaming — all features opencode's server already exposes. Without this change, opencode remains second-class: every step starts cold, every review inherits the implementation session's context, the operator cannot see streaming output in real time, and crash recovery means restarting from a blank slate.

## Dependencies

This change depends on `pi-rpc-integration` and MUST be applied after it. The pi-rpc-integration change establishes the shared agent contract (`RPCAgent` interface in `internal/agent/adapter.go`), the configuration system, the pipeline's smart-path routing, and TUI visibility. This change implements the opencode-specific adapter against those foundations.

## Scope

- Add an opencode HTTP server adapter that starts `opencode serve` as a managed subprocess, discovers its port, waits for health, and exposes the full server lifecycle (startup, session creation, prompt, streaming, abort, shutdown).
- Implement the `RPCAgent` interface (defined by `pi-rpc-integration` in `internal/agent/adapter.go`) so the pipeline can treat opencode and Pi as interchangeable smart-path agents behind one interface.
- Implement persistent implementation sessions for apply and fix, disposable review sessions for each review pass, and per-step model switching via the `model` parameter on each message.
- Stream SSE events from `GET /global/event` to the TUI, coalescing bursts before forwarding.
- Handle permission requests from opencode via `POST /session/:id/permissions/:permissionID` with auto-approve policy.
- Implement crash recovery by restarting `opencode serve`, recreating sessions, and re-prompting the current phase from disk.
- Register `opencode-rpc` as a distinct agent name selectable via `--agent opencode-rpc`, integrating with pi-rpc-integration's agent resolution.
- Preserve the existing CLI shell-out adapter as the dumb-path fallback when the user explicitly selects `opencode` without RPC mode.

## Non-Goals

- Not replacing or modifying the Pi RPC adapter. Both adapters coexist behind the same agent contract owned by `pi-rpc-integration`.
- Not using the ACP protocol or the TypeScript SDK. This change targets the HTTP server API exclusively.
- Not adding configuration, init wizard, or review intelligence changes. Those belong to `pi-rpc-integration`.
- Not changing the pipeline's phase progression, branch setup, or git operations. The pipeline's smart-path routing (checking `AsRPC()` in `runPhase`) is owned by `pi-rpc-integration`; this change only implements the opencode side of that routing.
- Not adding new TUI panels or controls beyond streaming events through pi-rpc's visibility layer.
- Not implementing structured output or slash command execution through the server API in this change. Those are optimizations for later.
