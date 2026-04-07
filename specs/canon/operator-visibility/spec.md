# Operator Visibility

## Requirements

### Requirement: Execution Context Visibility

The TUI SHALL show the resolved execution context for the active step, including the mode (`Nelson` or `Ralph`), the selected agent, the active model, and whether the pipeline resumed from a partially completed change.

#### Scenario: Pi review step

- **WHEN** nelsonctl is running a Pi-backed review step
- **THEN** the TUI shows Nelson mode, `pi`, and the configured review model for that step

#### Scenario: CLI fallback step

- **WHEN** nelsonctl is running with an explicitly selected CLI agent
- **THEN** the TUI shows Ralph mode and the selected CLI agent name

### Requirement: Pi Event Rendering

The TUI SHALL render coalesced Pi output updates without blocking pipeline progress, while still surfacing non-stream events such as retries, session restarts, and the Nelson taunt.

#### Scenario: Coalesced output

- **WHEN** Pi emits many incremental message updates during apply
- **THEN** the output panel updates smoothly and the pipeline continues processing terminal events immediately

#### Scenario: Pi restart notice

- **WHEN** nelsonctl restarts Pi after a crash
- **THEN** the TUI surfaces a restart event before the current phase is re-prompted

### Requirement: Exit Summary Includes Mode

The TUI MUST display a summary on exit showing phases completed, phases failed, total attempts, total duration, branch name, and the execution mode used for the run.

#### Scenario: Resume exit summary

- **WHEN** a resumed run finishes or aborts
- **THEN** the summary includes the branch name and whether the run executed in Nelson mode or Ralph mode

### Requirement: Controller Activity Visibility

The TUI SHALL display controller activity as status lines generated mechanically from the controller's tool call names. No additional model or parsing is required. The full controller reasoning SHALL be written to the trace log, not the TUI.

#### Scenario: Controller drafting prompt

- **WHEN** the controller calls `submit_prompt`
- **THEN** the TUI displays "⚙ Controller: sending apply prompt..."

#### Scenario: Controller running review

- **WHEN** the controller calls `run_review`
- **THEN** the TUI displays "⚙ Controller: running review..."

#### Scenario: Controller approving

- **WHEN** the controller calls `approve` with a summary
- **THEN** the TUI displays "⚙ Controller: approved — {summary}"

#### Scenario: Controller analyzing

- **WHEN** the controller is processing between tool calls (reasoning)
- **THEN** the TUI displays "⚙ Controller: analyzing..."
