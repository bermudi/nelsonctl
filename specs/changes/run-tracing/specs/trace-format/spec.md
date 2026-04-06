# Trace Format

## ADDED Requirements

### Requirement: JSONL Line Schema
Every line in a trace file SHALL be a valid JSON object containing at minimum a `type` field identifying the event kind and a `ts` field with an RFC 3339 nano timestamp. The system MUST NOT write partial JSON lines. Each JSON line SHALL be terminated with a newline character before the next line is written.

#### Scenario: Event structure
- **WHEN** any event is written to the trace
- **THEN** the line parses as JSON and contains both `type` and `ts` fields

### Requirement: Event Type Registry
The system SHALL define the following event types with their required fields:

| Event Type | Required Fields | Description |
|------------|----------------|-------------|
| `run_meta` | `version`, `agent`, `change`, `hostname`, `started_at` | Run identification header |
| `state_change` | `state`, `branch_reused` (optional, `Branch` state only) | Pipeline state machine transition |
| `phase_start` | `phase`, `name`, `attempt` | Phase execution begins |
| `agent_invoke` | `agent`, `prompt`, `work_dir` | Agent CLI or RPC call begins |
| `agent_result` | `exit_code`, `duration_ms`, `stdout_len`, `stderr_len` | Agent call completes |
| `output_chunk` | `chunk` | Agent stdout fragment |
| `review_result` | `passed`, `output`, `attempt`, `step` (`"phase"` or `"final"`), `phase` (present and non-empty when `step` is `"phase"`, omitted when `step` is `"final"`) | Review pass completes |
| `phase_result` | `phase`, `passed`, `attempts`, `review` | Phase outcome (final) |
| `git_commit` | `subject`, `files` | Git commit made |
| `git_push` | `remote`, `branch` | Branch pushed |
| `pr_created` | `title`, `url` | Pull request created |
| `taunt` | `phase` | Phase exhausted retries |
| `summary` | `phases_completed`, `phases_failed`, `duration`, `branch` | Pipeline summary |
| `run_end` | `status`, `duration_ms`, `phases_completed`, `phases_failed`, `error` (optional) | Run concludes |

Unknown event types SHALL be ignored by consumers without error.

#### Scenario: All events typed
- **WHEN** a complete run trace is inspected
- **THEN** every line has a `type` value from the defined registry

#### Scenario: review_result for phase review
- **WHEN** a phase review completes on the second attempt
- **THEN** the trace contains `{"type":"review_result","passed":true,"output":"...","attempt":2,"step":"phase","phase":1,"ts":"..."}`

#### Scenario: review_result for final review
- **WHEN** the final review completes
- **THEN** the trace contains `{"type":"review_result","passed":false,"output":"...","attempt":1,"step":"final","ts":"..."}`

#### Scenario: phase_result is the final phase outcome
- **WHEN** a phase finishes (after all retry attempts)
- **THEN** the trace contains `{"type":"phase_result","phase":1,"passed":true,"attempts":2,"review":"...","ts":"..."}`

### Requirement: Forward-Compatible Envelope
The event schema SHALL be forward-compatible: consumers MUST tolerate unknown fields, and new event types (e.g. `tool_call`, `thinking`, `token_usage` from future RPC integration) SHALL be writable without modifying the reader. Future RPC events MUST be routed through the same fan-out point as current events to ensure the TraceWriter receives them.

#### Scenario: Unknown fields ignored
- **WHEN** a consumer reads a trace that includes fields it does not recognize
- **THEN** the consumer processes known fields without error

#### Scenario: Future event types
- **WHEN** a future version of nelsonctl emits `tool_call` events alongside the current event types
- **THEN** older consumers skip those lines without breaking

### Requirement: Human-Readable Timestamps
All timestamps in trace files SHALL use RFC 3339 nano format (e.g. `2026-04-05T23:45:00.123456789Z`) so they are both machine-parseable and human-readable.

#### Scenario: Timestamp format
- **WHEN** any event is written
- **THEN** the `ts` field is a valid RFC 3339 nano string
