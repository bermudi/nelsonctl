# Review Intelligence

## ADDED Requirements

### Requirement: Independent Review Sessions
The system SHALL construct review prompts that always run in a fresh reviewer context. In Pi mode, each phase review and the final review MUST use a disposable review session and the configured review model, while apply and fix continue in the separate implementation session.

#### Scenario: Phase review in Pi mode
- **WHEN** a phase finishes apply in Pi mode
- **THEN** nelsonctl creates a fresh review session, switches to the configured review model, and prompts for an implementation review

#### Scenario: Final review in CLI mode
- **WHEN** all phases are complete and the effective agent is CLI-only
- **THEN** nelsonctl builds the scaffold's pre-archive review prompt and shells out once for the final review

### Requirement: Parsed Retry Loop
The system SHALL retry the fix-review cycle up to 3 total attempts per review gate. In Pi mode, nelsonctl MUST steer the long-lived implementation session with a compact list of parsed issues; in CLI mode, it MUST build a fresh fix prompt from the parsed issues and re-run the selected CLI agent.

#### Scenario: Pi fix steering
- **WHEN** a Pi review fails on the first attempt
- **THEN** nelsonctl sends the parsed issues into the existing apply session and re-runs review with a fresh review session

#### Scenario: CLI fix retry
- **WHEN** a CLI review fails on the first attempt
- **THEN** nelsonctl shells out again with a fix prompt built from the parsed issues before re-running review

#### Scenario: Max retries exhausted
- **WHEN** 3 attempts have been made and the review still fails at or above the configured failure threshold
- **THEN** nelsonctl displays "HA-ha!", commits the current work, records the unresolved review output, and continues according to the pipeline policy

### Requirement: Three-Tier Review Detection
The system MUST determine pass or fail using a three-tier review parser: fuzzy structured parsing of the litespec-review report, heuristic fallback for unstructured output, and optional controller-AI summarization when the first two stages cannot confidently classify the result. If the issue sections conflict with the scorecard, nelsonctl MUST trust the parsed issues over the scorecard.

#### Scenario: Fuzzy structured parse
- **WHEN** the review output contains `### CRITICAL`, `### WARNING`, `### SUGGESTION`, and a scorecard table with inconsistent casing
- **THEN** nelsonctl still parses the sections and scorecard correctly

#### Scenario: Ambiguous output with controller available
- **WHEN** the review output does not match the structured format and heuristics remain inconclusive
- **THEN** nelsonctl asks the configured controller model to summarize the issues into a compact machine-readable result before deciding pass or fail

#### Scenario: Issue-scorecard conflict
- **WHEN** the scorecard marks correctness as pass but the parsed issue sections include a CRITICAL finding
- **THEN** nelsonctl treats the review as failed

### Requirement: Review Failure Threshold
The system SHALL support `review.fail_on` values of `critical`, `warning`, and `suggestion`, with `critical` as the default. Findings below the configured threshold SHALL not trigger the retry loop.

#### Scenario: Warning below threshold
- **WHEN** the review output contains only WARNING findings and `review.fail_on` is `critical`
- **THEN** nelsonctl treats the review as passed

#### Scenario: Suggestion threshold
- **WHEN** the review output contains SUGGESTION findings and `review.fail_on` is `suggestion`
- **THEN** nelsonctl treats the review as failed and enters retry

### Requirement: Controller Summarizer
The system SHALL support OpenAI Responses API, Anthropic Messages API, and OpenRouter Chat Completions as raw HTTP backends for ambiguous review summarization. It MUST only call the controller after fuzzy parsing and heuristics fail to produce a confident result.

#### Scenario: OpenRouter summarizer
- **WHEN** the controller provider resolves to OpenRouter and the review output is ambiguous
- **THEN** nelsonctl sends the raw review text to the configured OpenRouter model and uses the summarized issues in the retry decision

#### Scenario: No controller configured
- **WHEN** the review output is ambiguous and no controller provider can be used
- **THEN** nelsonctl falls back to the heuristic result instead of failing startup mid-run
