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
| `execution_context` | `mode`, `agent`, `step` (optional), `model` (optional), `resumed` | Execution context for current step |
| `controller_activity` | `tool` (optional), `summary` (optional), `analyzing` | Controller tool dispatch |
| `phase_start` | `phase`, `name`, `attempt` | Phase execution begins |
| `agent_invoke` | `agent`, `step`, `model`, `session_id` (optional), `prompt`, `work_dir` | Agent invocation begins (pipeline level) |
| `agent_result` | `exit_code`, `duration_ms`, `stdout_len`, `stderr_len` | Agent call completes |
| `output_chunk` | `chunk` | Agent stdout fragment |
| `session_created` | `session_id`, `session_type` (`impl`/`review`), `parent_session` (optional, review sessions) | Pi adapter session created |
| `session_switched` | `session_id` | Pi adapter switched active session |
| `model_set` | `provider`, `model`, `success` | Pi adapter model selection result |
| `rpc_event` | `rpc_type`, `stop_reason` (optional), `session_id` (optional) | Pi adapter raw RPC event summary |
| `events_drained` | `count` | Pi adapter stale event drain |
| `agent_restarted` | `cause` | Pi adapter crash recovery |
| `review_result` | `passed`, `output`, `attempt`, `step` (`"phase"` or `"final"`), `phase` (present and non-empty when `step` is `"phase"`, omitted when `step` is `"final"`) | Review pass completes |
| `phase_result` | `phase`, `passed`, `attempts`, `review` | Phase outcome (final) |
| `git_commit` | `subject`, `files` | Git commit made |
| `git_push` | `remote`, `branch` | Branch pushed |
| `pr_created` | `title`, `url` | Pull request created |
| `taunt` | `phase` | Phase exhausted retries |
| `summary` | `phases_completed`, `phases_failed`, `duration`, `branch`, `total_attempts` (optional), `mode` (optional), `resumed` (optional) | Pipeline summary |
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

#### Scenario: Agent invocation event
- **WHEN** an agent is invoked for a step
- **THEN** the trace contains `{"type":"agent_invoke","agent":"pi","step":"apply","model":"opencode-go/kimi-k2.5","session_id":"...","prompt":"...","work_dir":"...","ts":"..."}` followed by `{"type":"agent_result","exit_code":0,"duration_ms":45230,"stdout_len":8432,"stderr_len":0,"ts":"..."}`

#### Scenario: Session created for implementation
- **WHEN** the Pi adapter creates or reuses an implementation session
- **THEN** the trace contains `{"type":"session_created","session_id":"...","session_type":"impl","ts":"..."}`

#### Scenario: Session created for review
- **WHEN** the Pi adapter creates a review session forked from the implementation session
- **THEN** the trace contains `{"type":"session_created","session_id":"...","session_type":"review","parent_session":"/path/to/impl/session.json","ts":"..."}`

#### Scenario: Model set successfully
- **WHEN** the Pi adapter sets the model for a session
- **THEN** the trace contains `{"type":"model_set","provider":"opencode-go","model":"kimi-k2.5","success":true,"ts":"..."}`

#### Scenario: Model set failure
- **WHEN** the Pi adapter fails to set the model
- **THEN** the trace contains `{"type":"model_set","provider":"minimax","model":"minimax-m2.7","success":false,"ts":"..."}`

#### Scenario: RPC event summary
- **WHEN** the Pi adapter receives an `agent_end` RPC event with `stopReason: "toolUse"`
- **THEN** the trace contains `{"type":"rpc_event","rpc_type":"agent_end","stop_reason":"toolUse","session_id":"...","ts":"..."}`

#### Scenario: Events drained
- **WHEN** the Pi adapter drains stale events before consuming new ones
- **THEN** the trace contains `{"type":"events_drained","count":4,"ts":"..."}`

#### Scenario: Agent restarted after crash
- **WHEN** the Pi adapter detects a crash and restarts the Pi process
- **THEN** the trace contains `{"type":"agent_restarted","cause":"pi process exited unexpectedly","ts":"..."}`

#### Scenario: Session switched
- **WHEN** the Pi adapter switches the active session
- **THEN** the trace contains `{"type":"session_switched","session_id":"...","ts":"..."}`

#### Scenario: Execution context event
- **WHEN** the pipeline emits execution context for a step
- **THEN** the trace contains `{"type":"execution_context","mode":"nelson","agent":"pi","step":"apply","model":"opencode-go/kimi-k2.5","resumed":false,"ts":"..."}`

#### Scenario: Controller activity event
- **WHEN** the controller dispatches a tool call
- **THEN** the trace contains `{"type":"controller_activity","tool":"submit_prompt","ts":"..."}`

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
