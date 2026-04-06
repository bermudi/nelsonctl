# Agent Adapter

## ADDED Requirements

### Requirement: Two-Tier Agent Interface
The system SHALL define a pipeline-facing `Agent` interface in `internal/agent/adapter.go` that all execution agents MUST implement. The interface SHALL provide methods for step execution, prerequisite checks, and cleanup. CLI adapters and RPC adapters MUST both satisfy this interface so the pipeline can treat them interchangeably.

#### Scenario: CLI agent implements Agent
- **WHEN** nelsonctl resolves the effective agent as a CLI adapter (`opencode`, `claude`, `codex`, or `amp`)
- **THEN** the pipeline uses the `Agent` interface to invoke the one-command-per-step loop

#### Scenario: RPC agent implements Agent
- **WHEN** nelsonctl resolves the effective agent as an RPC adapter (`pi` or `opencode-rpc`)
- **THEN** the pipeline uses the `Agent` interface for the outer contract while the RPC-specific behavior is accessed via `AsRPC()`

### Requirement: RPCAgent Extension Interface
The system SHALL define an `RPCAgent` interface in `internal/agent/adapter.go` that extends `Agent` with session management, persistent messaging, and abort capability. An `Agent` implements `RPCAgent` only if it supports persistent sessions across steps. The interface SHALL include:

- `AsRPC() *RPCAgent` — returns the RPC interface if the agent supports persistent sessions, or `nil` for CLI-only agents
- `StartImplementationSession(ctx) (sessionID, error)` — creates or returns the long-lived session for apply and fix steps
- `StartReviewSession(ctx) (sessionID, error)` — creates a fresh disposable session for a review pass
- `SendMessage(ctx, sessionID, prompt, model) (result, error)` — sends a prompt to the session and blocks until completion
- `Abort(ctx, sessionID) error` — terminates an in-progress message call
- `Events() <-chan Event` — returns a channel for real-time output events forwarded to the TUI
- `Close() error` — shuts down the RPC process and cleans up resources

#### Scenario: Pipeline detects RPC capability
- **WHEN** the pipeline receives a resolved agent and calls `AsRPC()`
- **THEN** the pipeline receives a non-nil RPCAgent for `pi` and `opencode-rpc`, and `nil` for CLI adapters

#### Scenario: Pipeline uses smart path for RPC agents
- **WHEN** `AsRPC()` returns a non-nil interface
- **THEN** the pipeline uses `StartImplementationSession`, `SendMessage`, `Abort`, and `StartReviewSession` instead of the one-command-per-step loop

#### Scenario: Pipeline uses dumb path for CLI agents
- **WHEN** `AsRPC()` returns `nil`
- **THEN** the pipeline falls back to the shell-out execution path

### Requirement: Agent Registration
The system SHALL provide a registration mechanism in `internal/agent/adapter.go` that maps agent names to their constructors. The registry MUST allow built-in agents (`pi`, `opencode`, `claude`, `codex`, `amp`) to be registered at init time and allow downstream changes to register additional agents without modifying the core registry code.

#### Scenario: Built-in agent registration
- **WHEN** nelsonctl starts and loads the agent package
- **THEN** the registry contains constructors for `pi`, `opencode`, `claude`, `codex`, and `amp`

#### Scenario: Downstream agent registration
- **WHEN** a downstream change adds an `opencode-rpc` agent
- **THEN** the registration mechanism allows the agent to be added via an init function or explicit registration call without editing `adapter.go`

#### Scenario: Agent resolution by name
- **WHEN** the pipeline resolves the agent name from config or CLI flags
- **THEN** the registry returns the corresponding agent constructor for instantiation

### Requirement: Agent Interface Methods
The `Agent` interface SHALL define the following methods that all agents MUST implement:

- `Name() string` — returns the agent's canonical name for logging and error messages
- `CheckPrerequisites(ctx) error` — validates that the agent binary or service is available before pipeline execution
- `ExecuteStep(ctx, step, prompt, model) (result, error)` — runs one pipeline step and returns structured output; CLI adapters invoke a shell command, RPC adapters delegate to their session implementation
- `Cleanup(ctx) error` — releases resources after pipeline completion or abort

#### Scenario: CLI ExecuteStep shells out
- **WHEN** a CLI agent receives an `ExecuteStep` call
- **THEN** it builds and invokes a shell command, captures stdout/stderr, and returns the result without persistent session state

#### Scenario: RPC ExecuteStep creates session if needed
- **WHEN** an RPC agent receives an `ExecuteStep` call without a prior `StartImplementationSession`
- **THEN** it creates the implementation session before sending the message

#### Scenario: Prerequisite check fails early
- **WHEN** `CheckPrerequisites` returns an error
- **THEN** the pipeline exits before phase execution with an agent-specific installation message

### Requirement: Shared Event Type
The system SHALL define an `Event` struct in `internal/agent/adapter.go` that represents a single output event from any agent. The struct MUST include the event type (text, error, completion), the content, and optional metadata for TUI rendering. Both CLI and RPC adapters MUST emit events through the `Events()` channel using this shared type.

#### Scenario: CLI emits text events
- **WHEN** a CLI adapter captures stdout from a subprocess
- **THEN** it wraps each chunk as an `Event{Type: TextEvent, Content: chunk}` and sends it to the events channel

#### Scenario: RPC agent emits structured events
- **WHEN** an RPC adapter receives a Pi or opencode event
- **THEN** it translates the native event format to `Event{Type: ..., Content: ..., Metadata: ...}` before forwarding