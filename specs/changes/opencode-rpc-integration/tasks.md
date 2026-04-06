# Tasks

**Prerequisite**: `pi-rpc-integration` must be applied first. This change assumes the `RPCAgent` interface, configuration system, pipeline smart-path routing, and agent resolution are already in place.

## Phase 1: HTTP Client and Server Process
- [ ] Implement `internal/agent/occlient.go` — typed HTTP client for `GET /global/health`, `POST /session`, `GET /session/:id`, `DELETE /session/:id`, `POST /session/:id/message`, `POST /session/:id/abort`, `POST /session/:id/permissions/:permissionID`, `POST /instance/dispose`. Request/response structs, error handling, optional basic auth. Supports all session management and message sending requirements.
- [ ] Implement `internal/agent/ocserver.go` — start `opencode serve --port 0` as a child process (or `opencode web` for development), probe `GET /global/health` with backoff until healthy, capture the assigned port, and shut down via `POST /instance/dispose` + process termination with grace period. Supports `Managed Server Process`.
- [ ] Implement `internal/agent/ocsse.go` — SSE client that reads `GET /global/event`, parses event frames, coalesces text deltas onto a configurable render tick, and forwards typed events to a callback. Runs on a separate goroutine from blocking message calls. Supports `SSE Event Consumption` and `Event Coalescing`.
- [ ] Write `internal/agent/occlient_test.go` — tests for the HTTP client covering health check, session CRUD, message round-trip, abort, and permission approval using a mock HTTP server.
- [ ] Write `internal/agent/ocserver_test.go` — tests for server startup, health probing, port discovery, clean shutdown, and crash detection using a mock server process.
- [ ] Write `internal/agent/ocsse_test.go` — tests for SSE parsing, event coalescing, render tick batching, and goroutine safety.

## Phase 2: RPC Adapter
- [ ] Implement `internal/agent/opencode-rpc.go` — the opencode RPC adapter that implements both `Agent` (from pi-rpc's `internal/agent/adapter.go`) and `RPCAgent`. Creates one implementation session for apply/fix, disposable review sessions, maps pipeline steps to server API calls, extracts response text from message parts into `Result.Stdout`, and auto-approves all permission requests (file edits, bash commands, and all other tool calls without restriction). Supports `OpenCode RPC Adapter`, `Persistent Implementation Session`, `Disposable Review Sessions`, `Per-Step Model Selection`, and `Permission Auto-Approval`.
- [ ] Register `opencode-rpc` as a distinct agent name in the agent resolution system (established by pi-rpc-integration), so `--agent opencode-rpc` selects the RPC adapter. The existing `opencode` agent name continues to use CLI shell-out. Supports `OpenCode Prerequisite Check`.

## Phase 3: Crash Recovery
- [ ] Implement crash recovery in `opencode-rpc.go` — when the server process exits unexpectedly, restart it, recreate the required session topology, and re-prompt the current phase from disk state. Supports `Crash Recovery`.

## Phase 4: Tests and Verification
- [ ] Write `internal/agent/opencode-rpc_test.go` — integration tests with a mock HTTP server covering session lifecycle, apply/fix session reuse, review session isolation, model switching, abort, permission auto-approval, and crash recovery.
- [ ] Run existing test suite to verify CLI adapters are unaffected by the adapter registration.
- [ ] Verify that `opencode-rpc` implements the exact `Agent` and `RPCAgent` interface signatures defined in pi-rpc-integration's agent-adapter spec, including all method signatures, return types, and error behavior.
