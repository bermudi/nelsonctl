# opencode-rpc-integration — Design Document

This change adds an opencode RPC adapter that talks to `opencode serve` over HTTP, giving nelsonctl persistent sessions, per-step model selection, SSE streaming, and abort capability. The adapter coexists with the existing CLI adapters and the Pi RPC adapter behind the shared agent contract established by `pi-rpc-integration`.

**Dependency**: This change requires `pi-rpc-integration` to be applied first. The `RPCAgent` interface, configuration system, pipeline smart-path routing, and TUI visibility are all owned by pi-rpc-integration.

## Architecture

The adapter lives in a new layer between the pipeline and the opencode HTTP server:

1. **Server process manager** (`internal/agent/ocserver.go`) — starts `opencode serve --port 0`, probes for health, and shuts it down. Optionally connects to an externally managed server when a URL is configured.

2. **HTTP client** (`internal/agent/occlient.go`) — typed Go client for the opencode server API. Covers session CRUD, message sending, abort, permission approval, and health checks. No code generation; hand-written against the server docs to keep the dependency footprint at zero.

3. **SSE consumer** (`internal/agent/ocsse.go`) — reads `GET /global/event`, parses SSE frames into typed Go events, and fans them out to the TUI via the existing `EventHandler` callback. Coalesces bursty text deltas onto a configurable render tick.

4. **RPC adapter** (`internal/agent/opencode-rpc.go`) — implements the `Agent` and `RPCAgent` interfaces (defined by pi-rpc-integration in `internal/agent/adapter.go`). Creates one implementation session for the apply-fix loop and disposable review sessions for each review pass. Maps pipeline steps to server API calls and extracts `Result.Stdout` from response parts.

5. **Agent registration** — registers `opencode-rpc` as a distinct agent name that pi-rpc-integration's agent resolution can select via `--agent opencode-rpc`. The existing `opencode` agent name remains the CLI shell-out path.

## Decisions

### HTTP server over ACP or SDK

The HTTP server API was chosen because it is the most complete opencode integration surface, requires zero protocol work (plain `net/http`), and nelsonctl can auto-generate types from the OpenAPI spec later if needed. ACP would require implementing JSON-RPC 2.0 framing and ACP-specific type structs for no additional capability. The TypeScript SDK is not usable from Go.

### Managed server lifecycle

nelsonctl owns the server process. Starting `opencode serve --port 0` gives a dynamically assigned port that the adapter discovers via health probe. This avoids port conflicts and mirrors the TS SDK's `createOpencode()` pattern. An external URL option is provided via pi-rpc-integration's configuration system (`opencode_server_url` in config.yaml) for users who already run `opencode serve` or `opencode web`. When an external URL is configured, the adapter skips process startup and defers shutdown to the external owner.

### Hand-written HTTP client

No OpenAPI code generation in this change. The server API surface needed for the smart path is small (~10 endpoints). A hand-written client keeps the Go dependency graph clean and avoids a build-time codegen step. If the server API grows or changes frequently, code generation can be added later.

### Synchronous messages, streaming via SSE

`POST /session/:id/message` blocks until the assistant finishes, which simplifies the pipeline's step loop. The SSE stream (`GET /global/event`) provides real-time output for the TUI while the message call is in flight. The SSE consumer runs on a **separate goroutine** from the blocking message call, so streaming events are forwarded to the TUI via the `EventHandler` callback without blocking the pipeline's step loop. This avoids the complexity of async message + polling for completion.

### Separate sessions for apply and review

Apply and fix share one session to preserve implementation context. Review uses a fresh session per pass to avoid bias. This matches the Pi RPC design and keeps review independent.

### Auto-approve permissions

The pipeline runs unattended, so opencode permission requests are auto-approved. Each grant is logged to the TUI for visibility. This includes **all** tool permissions — file edits, bash command execution, and any other tool calls — without restriction. This is an accepted risk for pipeline mode: nelsonctl already scopes file operations via git staging, and bash commands run in the same trust boundary as the agent itself. A future change may introduce tiered approval (auto-approve file edits, prompt for destructive commands) if pipeline safety requirements evolve.

## File Changes

- `internal/agent/opencode-rpc.go` — implement the `Agent` and `RPCAgent` interfaces (defined by pi-rpc-integration in `internal/agent/adapter.go`). Manages one implementation session and disposable review sessions. Extracts response text from server message parts. Supports `OpenCode RPC Adapter`.

- `internal/agent/ocserver.go` — start/stop `opencode serve` (or `opencode web` for development/debugging), health probe with timeout, process group cleanup. When `opencode_server_url` is set in config, skips startup and connects to the external server. Supports `Managed Server Process`.

- `internal/agent/occlient.go` — typed HTTP client for session CRUD (`POST/GET/DELETE /session`), message sending (`POST /session/:id/message`), abort (`POST /session/:id/abort`), permission approval (`POST /session/:id/permissions/:permissionID`), health check (`GET /global/health`), and server disposal (`POST /instance/dispose`). Supports all session management and streaming requirements.

- `internal/agent/ocsse.go` — SSE client that reads `GET /global/event`, parses event types, coalesces text deltas, and forwards to the pipeline event handler. Runs on a separate goroutine from blocking message calls. Supports `SSE Event Consumption` and `Event Coalescing`.

- `internal/agent/opencode-rpc_test.go` — tests for server lifecycle, session management, response extraction, crash recovery, and event coalescing using a mock HTTP server.

- `internal/agent/occlient_test.go` — tests for the HTTP client covering all endpoint calls.

- `internal/agent/ocserver_test.go` — tests for server process management, health probing, and shutdown lifecycle.

- `internal/agent/ocsse_test.go` — tests for SSE parsing, event coalescing, and goroutine safety.
