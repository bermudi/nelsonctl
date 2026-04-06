# Session Management

## ADDED Requirements

### Requirement: Persistent Implementation Session
The adapter SHALL create one long-lived opencode session for apply and fix work across an entire change run. The implementation session MUST be reused when a phase fails review and enters the fix loop, so that the fix prompt inherits the apply context.

#### Scenario: Apply creates the session
- **WHEN** the first phase begins its apply step
- **THEN** the adapter creates a new opencode session via `POST /session` and sends the apply prompt via `POST /session/:id/message`

#### Scenario: Fix reuses the session
- **WHEN** a phase fails review and the pipeline builds a fix prompt
- **THEN** the adapter sends the fix prompt into the same implementation session used for apply, preserving the conversation context

### Requirement: Disposable Review Sessions
The adapter SHALL create a fresh opencode session for each phase review and the final review. Review sessions MUST NOT share conversation history with the implementation session or with each other.

#### Scenario: Phase review starts fresh
- **WHEN** a phase finishes apply and the pipeline runs the review step
- **THEN** the adapter creates a new session, sends the review prompt, and discards the session when the review completes

#### Scenario: Final review starts fresh
- **WHEN** all phases are complete and the pipeline runs the final review
- **THEN** the adapter creates a new session for the final review independent of all previous sessions

### Requirement: Per-Step Model Selection
The adapter SHALL accept a model selection for each pipeline step and pass it as the `model` parameter on every `POST /session/:id/message` call. The model MAY differ between apply, review, and fix steps.

#### Scenario: Review uses a different model
- **WHEN** a separate review model is configured
- **THEN** the adapter passes `{ providerID, modelID }` matching the review model on review prompts and the apply/fix model on implementation prompts

#### Scenario: No model override
- **WHEN** no per-step model is configured
- **THEN** the adapter omits the `model` parameter and lets opencode use its default model

### Requirement: Session Abort
The adapter SHALL support aborting a running session via `POST /session/:id/abort` when the pipeline times out, the operator triggers a manual abort, or the agent exceeds the configured step timeout.

#### Scenario: Step timeout
- **WHEN** an apply or review step exceeds its configured timeout
- **THEN** the adapter calls abort on the session and reports the step as failed

#### Scenario: Manual abort
- **WHEN** the operator presses the abort keybinding
- **THEN** the adapter aborts the active session, preserves on-disk work, and exits cleanly

### Requirement: Permission Auto-Approval
The adapter SHALL respond to opencode permission requests by auto-approving them via `POST /session/:id/permissions/:permissionID` so that the pipeline is not blocked waiting for human input. The adapter MUST log each permission grant for audit visibility.

#### Scenario: Tool permission requested
- **WHEN** opencode emits a permission request event during a pipeline step
- **THEN** the adapter responds with approval and emits an output event to the TUI showing what was approved
