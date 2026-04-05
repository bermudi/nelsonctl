# TUI

## ADDED Requirements

### Requirement: Layout
The TUI SHALL display a two-panel layout: a left panel showing pipeline progress and a right panel streaming agent output.

#### Scenario: Running
- **WHEN** the pipeline is executing
- **THEN** the left panel shows phases with progress indicators and the right panel streams agent output

### Requirement: Phase Progress
The left panel SHALL show each phase with its status: pending, running, passed, failed, or skipped.

#### Scenario: Phase indicators
- **WHEN** a phase is in progress
- **THEN** it displays a spinner; completed phases show ✓ or ✗ with attempt count

### Requirement: Retry Counter
The TUI SHALL display the current attempt number for the active phase (e.g., "attempt 2/3").

#### Scenario: Retry display
- **WHEN** the system is on retry 2 of a phase
- **THEN** the left panel shows "attempt 2/3" next to the phase

### Requirement: Nelson Taunt
The TUI MUST display a prominent "HA-ha!" message (Nelson Muntz style) when a phase exhausts all 3 attempts and still fails.

#### Scenario: Max retries taunt
- **WHEN** a phase fails after 3 total attempts
- **THEN** the TUI displays "HA-ha!" in the output panel before proceeding

### Requirement: Keybindings
The TUI SHALL support keybindings for abort (q/ctrl+c), pause (p), and scroll (j/k or arrow keys) in the output panel.

#### Scenario: Abort
- **WHEN** the user presses q or ctrl+c
- **THEN** the system terminates the current agent, commits progress, and exits

#### Scenario: Pause
- **WHEN** the user presses p during execution
- **THEN** the system waits for the current agent invocation to finish, then pauses before the next step

### Requirement: Summary on Exit
The TUI MUST display a summary on exit showing: phases completed, phases failed, total attempts, total duration, and the branch name.

#### Scenario: Normal exit
- **WHEN** the pipeline completes or is aborted
- **THEN** the TUI prints a summary table before exiting
