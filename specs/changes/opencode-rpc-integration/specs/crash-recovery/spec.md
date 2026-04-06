# Crash Recovery

## ADDED Requirements

### Requirement: Crash Recovery
When the opencode server process exits unexpectedly during pipeline execution, the adapter SHALL restart the server, recreate the required session topology, and re-prompt the current phase from disk state. The adapter MUST NOT lose on-disk work completed before the crash. Recovery attempts are limited to a configurable maximum (`opencode.crash_retries` in `config.yaml`, default: 3).

#### Scenario: Server crashes during apply step
- **WHEN** the opencode server process exits while an apply message is in flight
- **THEN** the adapter restarts the server, creates a new implementation session, sends the apply prompt with the current phase's context from disk, and continues the pipeline

#### Scenario: Server crashes during review step
- **WHEN** the opencode server process exits while a review message is in flight
- **THEN** the adapter restarts the server, creates a new disposable review session, sends the review prompt, and continues the pipeline

#### Scenario: Server crashes during fix step
- **WHEN** the opencode server process exits while a fix message is in flight
- **THEN** the adapter restarts the server, creates a new implementation session (fix context is not preserved across crashes), sends the fix prompt with the current phase's review issues from disk, and continues the pipeline

#### Scenario: Recovery exhausts retries
- **WHEN** the server crashes and the adapter cannot restart it within the configured retry limit (`opencode.crash_retries`, default: 3)
- **THEN** the adapter reports a fatal error, preserves any on-disk work, and exits the pipeline cleanly
