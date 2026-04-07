# Smart Agent Mode

## ADDED Requirements

### Requirement: Two-Tier Agent Execution
The system SHALL support both CLI-only agents and RPC-capable agents behind a common pipeline-facing abstraction. The pipeline MUST detect at runtime whether the resolved agent supports persistent sessions and choose the smart or dumb execution path without requiring different user commands.

#### Scenario: Pi uses the smart path
- **WHEN** the effective agent is `pi`
- **THEN** the pipeline uses the RPC-aware path with persistent sessions

#### Scenario: Explicit CLI agent uses the dumb path
- **WHEN** the user explicitly selects `opencode`, `claude`, `codex`, or `amp`
- **THEN** the pipeline uses the CLI shell-out path for that run

### Requirement: Pi-First Agent Resolution
The system SHALL treat `pi` as the preferred execution agent when no non-Pi agent is explicitly selected. It MUST NOT silently fall back to a different CLI agent if `pi` is unavailable; instead it SHALL fail with setup instructions unless the user explicitly configured or requested a CLI agent.

#### Scenario: Pi available and no override
- **WHEN** `pi` is installed and no non-Pi agent was explicitly selected
- **THEN** nelsonctl runs in Pi RPC mode

#### Scenario: Explicit CLI override
- **WHEN** the user configures or passes a non-Pi agent selection
- **THEN** nelsonctl uses that CLI agent even if `pi` is installed

### Requirement: Agent Prerequisite Check
The system SHALL verify that the effective execution agent is installed before starting the pipeline, and it MUST validate that `pi` can be launched in RPC mode when Pi is selected.

#### Scenario: Pi missing
- **WHEN** Pi is the effective agent and the `pi` binary is not available
- **THEN** nelsonctl exits before the pipeline starts with installation guidance

#### Scenario: Explicit CLI agent missing
- **WHEN** the user explicitly selects `amp` and the binary is not on PATH
- **THEN** nelsonctl exits before the pipeline starts with an agent-specific error

### Requirement: Pi RPC Sessions
The system MUST start `pi` as a long-lived RPC process using `pi --mode rpc --no-extensions`. It SHALL keep one long-lived Pi session for apply and fix work across a change run, and it SHALL create a fresh disposable Pi review session for each phase review and the final review so that review feedback remains independent from the implementation session.

#### Scenario: Pi startup
- **WHEN** the selected agent is `pi`
- **THEN** nelsonctl launches `pi --mode rpc --no-extensions` in the workspace root before phase execution

#### Scenario: Fix reuses apply context
- **WHEN** a phase fails review and enters fix mode
- **THEN** nelsonctl sends the fix steering input into the existing apply session instead of creating a new implementation session

#### Scenario: Review starts fresh
- **WHEN** nelsonctl runs the phase review after apply completes
- **THEN** it creates a separate review session that does not share the apply conversation history

### Requirement: Streaming Agent Output
The system MUST stream the active agent's output to the TUI in real time while preserving the full transcript for review parsing and debugging. Pi `message_update` events SHALL be coalesced onto a short render tick before they are sent to the TUI.

#### Scenario: Pi event burst
- **WHEN** Pi emits many `message_update` events in rapid succession
- **THEN** the TUI receives batched output updates while the full text remains preserved in memory

#### Scenario: CLI stdout streaming
- **WHEN** a CLI agent writes stdout incrementally
- **THEN** stdout chunks appear in the TUI output panel as they arrive

### Requirement: Step Timeouts and Abort
The system operates with two tiers of timeout. At the controller level, the pipeline enforces a 45-minute conversation timeout and a 50-tool-call budget (see Controller Guardrails). At the agent level, individual `submit_prompt` and `run_review` calls have separate configurable timeouts for apply, review, and fix steps. A step-level timeout returns an error result to the controller, which can retry or call `approve`. A conversation-level timeout terminates the phase entirely. The system SHALL terminate the active Pi or CLI process when the operator aborts the run.

#### Scenario: Step timeout returns error to controller
- **WHEN** a `submit_prompt` or `run_review` call exceeds its configured step timeout
- **THEN** the agent layer terminates the current operation and returns an error result to the controller, which decides whether to retry or give up

#### Scenario: Conversation timeout terminates phase
- **WHEN** the controller conversation exceeds its 45-minute budget
- **THEN** the pipeline terminates the entire conversation and marks the phase as failed (see Controller Guardrails)

#### Scenario: Manual abort
- **WHEN** the user presses the abort keybinding
- **THEN** nelsonctl terminates the active agent process, preserves recoverable work on disk, and exits cleanly

### Requirement: Pi Crash Recovery
The system SHALL recover from an unexpected Pi process exit by starting a fresh Pi process, recreating the required session topology, and re-prompting the current phase from scratch. It MUST NOT attempt transcript restoration for the crashed session.

#### Scenario: Apply session crash
- **WHEN** the Pi process exits unexpectedly during apply or fix
- **THEN** nelsonctl restarts Pi and re-runs the current phase from its beginning using the files already written to disk

#### Scenario: Review session crash
- **WHEN** the Pi process exits unexpectedly during review
- **THEN** nelsonctl restarts Pi, recreates the disposable review session, and re-runs that review step
