# Agent Adapter

## Requirements

### Requirement: Agent CLI Abstraction

The system SHALL support multiple agent CLIs behind a common interface that accepts a prompt string and returns structured output.

#### Scenario: Supported agents

- **WHEN** the user specifies an agent via `--agent <name>`
- **THEN** the system accepts `opencode`, `claude`, `codex`, and `amp`

### Requirement: Default Agent

The system SHALL default to `opencode` when no `--agent` flag is provided.

#### Scenario: No flag

- **WHEN** `--agent` is omitted
- **THEN** the system uses `opencode`

### Requirement: Agent Invocation

The system MUST invoke each agent CLI with the correct flags for headless, non-interactive execution with structured output.

#### Scenario: opencode

- **WHEN** agent is `opencode`
- **THEN** the system runs `opencode run --format json <prompt>`

#### Scenario: claude

- **WHEN** agent is `claude`
- **THEN** the system runs `claude -p <prompt> --allowedTools Bash,Read,Edit --output-format json`

#### Scenario: codex

- **WHEN** agent is `codex`
- **THEN** the system runs `codex exec --json <prompt>`

#### Scenario: amp

- **WHEN** agent is `amp`
- **THEN** the system runs `amp --execute --stream-json <prompt>`

### Requirement: Agent Availability Check

The system SHALL verify the selected agent CLI is available on PATH before starting the pipeline.

#### Scenario: Agent not found

- **WHEN** the agent binary is not on PATH
- **THEN** the system exits with an error and an installation hint

### Requirement: Output Streaming

The system MUST stream the agent's stdout to the TUI in real time while also capturing the full output for review parsing.

#### Scenario: Streaming

- **WHEN** the agent is running
- **THEN** stdout chunks appear in the TUI output panel as they arrive

### Requirement: Timeout and Abort

The system SHALL enforce a configurable timeout per agent invocation (default: 10 minutes) and support manual abort via keybinding.

#### Scenario: Timeout

- **WHEN** the agent exceeds the timeout
- **THEN** the system sends SIGTERM, waits 250ms, then SIGKILL

#### Scenario: Manual abort

- **WHEN** the user presses the abort keybinding
- **THEN** the system terminates the agent and marks the attempt as failed
