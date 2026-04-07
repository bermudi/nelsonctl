# Review Intelligence

## ADDED Requirements

### Requirement: Independent Review Sessions
The system SHALL run reviews in a context independent from implementation. In Pi mode, each phase review and the final review MUST use a disposable review session and the configured review model. In CLI mode, each review MUST be a fresh shell-out invocation. The review prompt is mechanical and is NOT drafted by the controller AI.

#### Scenario: Phase review in Pi mode
- **WHEN** a phase finishes apply in Pi mode
- **THEN** nelsonctl creates a fresh review session, switches to the configured review model, and runs litespec-review

#### Scenario: Phase review in CLI mode
- **WHEN** a phase finishes apply in CLI mode
- **THEN** nelsonctl shells out with the mechanical review prompt

#### Scenario: Final review
- **WHEN** all phases are complete
- **THEN** nelsonctl runs the pre-archive review in a fresh session using the mechanical review prompt

### Requirement: Mechanical Review Prompt
The review prompt SHALL be a fixed template that invokes the litespec-review skill against the change's proposal. The controller AI MUST NOT draft or modify the review prompt. This ensures the reviewer operates as a cold read, independent of the controller's theory about what went wrong.

#### Scenario: Review prompt content
- **WHEN** the controller calls `run_review`
- **THEN** the pipeline sends a fixed prompt: "Use your litespec-review skill to review the implementation of change X against the proposal at specs/changes/X/proposal.md"

### Requirement: Review Failure Threshold
The system SHALL support `review.fail_on` values of `critical`, `warning`, and `suggestion`, with `critical` as the default. The threshold is injected into the controller AI's system prompt and applied through comprehension, not code-level severity parsing.

#### Scenario: Threshold in controller context
- **WHEN** the pipeline creates a controller conversation
- **THEN** the system prompt includes the configured `review.fail_on` value with instructions to only fail the review if issues at or above that severity are found

### Requirement: Review Output Handling
The raw review output from the agent MUST be returned to the controller AI without modification. The controller analyzes the output through comprehension — no regex matching, no structured parsing, no heuristic fallback. The controller decides pass or fail by calling `approve` or `submit_prompt` (for a fix).

#### Scenario: Controller receives raw review
- **WHEN** `run_review` completes
- **THEN** the full unmodified review text is returned to the controller conversation for analysis
