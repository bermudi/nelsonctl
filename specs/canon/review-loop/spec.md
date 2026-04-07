# Review Loop

## Requirements

### Requirement: Review Prompt Construction

The system SHALL construct a review prompt that instructs the agent to use its litespec-review skill, passing the change name and current context.

#### Scenario: Phase review

- **WHEN** a phase has just been applied
- **THEN** the prompt instructs the agent to run an implementation review for the change

#### Scenario: Final review

- **WHEN** all phases are complete
- **THEN** the prompt instructs the agent to run a pre-archive review for the change

### Requirement: Retry on Failure

The system SHALL retry the fix-review cycle up to 3 total attempts (1 initial + 2 retries) when a review identifies issues.

#### Scenario: First failure

- **WHEN** the review output indicates issues
- **THEN** the system crafts a fix prompt including the review feedback and re-runs the agent

#### Scenario: Max retries exhausted

- **WHEN** 3 attempts have been made and the review still fails
- **THEN** the system displays "HA-ha!" (Nelson Muntz style), commits what it has, logs the unresolved issues, and proceeds to the next phase

### Requirement: Review Result Detection

The system MUST determine pass/fail from the agent's output by scanning for explicit pass/fail signals in the review response.

#### Scenario: Pass detection

- **WHEN** the agent output contains no actionable issues
- **THEN** the system marks the phase as passed

#### Scenario: Fail detection

- **WHEN** the agent output contains actionable issues or errors
- **THEN** the system marks the phase as failed and enters retry
