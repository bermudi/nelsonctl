# Agent Integration

## ADDED Requirements

### Requirement: OpenCode RPC Adapter
The system SHALL include an opencode RPC adapter in `internal/agent/opencode-rpc.go` that implements the `Agent` and `RPCAgent` interfaces defined by `pi-rpc-integration` in `internal/agent/adapter.go`. The adapter exposes session creation, model switching, server startup/shutdown, and SSE streaming through the shared `RPCAgent` contract.

#### Scenario: Adapter selected via agent name
- **WHEN** nelsonctl resolves `opencode-rpc` as the execution agent via `--agent opencode-rpc`
- **THEN** the pipeline uses the opencode RPC adapter through pi-rpc-integration's smart-path routing

#### Scenario: Adapter falls back to CLI
- **WHEN** the user selects `--agent opencode` (without `-rpc`) or RPC is disabled
- **THEN** nelsonctl uses the existing CLI `opencode run` adapter

### Requirement: Agent Contract Adoption
The opencode RPC adapter SHALL implement the `RPCAgent` interface (owned by `pi-rpc-integration` in `internal/agent/adapter.go`) without modifying the interface itself. The pipeline's smart-path routing (`AsRPC()` check in `runPhase`) is owned by pi-rpc-integration; this change only provides the opencode implementation.

#### Scenario: Pipeline detects RPC capability
- **WHEN** the pipeline receives the opencode-rpc adapter and checks `AsRPC()`
- **THEN** the pipeline selects the smart execution path as routed by pi-rpc-integration

#### Scenario: CLI agent unchanged
- **WHEN** the effective agent is `opencode`, `claude`, `codex`, or `amp`
- **THEN** the pipeline uses the existing one-command-per-step loop with no session management

### Requirement: OpenCode Prerequisite Check
The adapter SHALL verify that `opencode` is installed and that `opencode serve` can start successfully before the pipeline begins. If the check fails, nelsonctl MUST exit with installation guidance.

#### Scenario: OpenCode not installed
- **WHEN** `opencode-rpc` is the effective agent and the binary is not on PATH
- **THEN** nelsonctl exits before the pipeline starts with a message explaining how to install opencode

#### Scenario: OpenCode installed
- **WHEN** opencode is on PATH
- **THEN** the adapter proceeds with server startup
