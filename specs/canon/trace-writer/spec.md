# Trace Writer

## Requirements

### Requirement: JSONL Trace Emission

The system SHALL write one JSON object per line to a trace file for every pipeline event emitted during a run. The trace writer MUST be a passive observer that receives the same events as the TUI without affecting pipeline behavior or timing. All serialization and file I/O SHALL occur on a background goroutine fed by a buffered channel so that the pipeline goroutine never blocks on trace writes.

#### Scenario: Normal run produces trace

- **WHEN** the pipeline runs to completion
- **THEN** a trace file exists at `$XDG_DATA_HOME/nelsonctl/traces/<timestamp>_<change>_<agent>.jsonl` containing one JSON line per pipeline event

#### Scenario: Failed run produces trace

- **WHEN** the pipeline fails at any point (branch creation, agent error, review failure)
- **THEN** a trace file exists with all events emitted up to the failure point, followed by a `run_end` event with `status: "error"`

#### Scenario: Trace disabled

- **WHEN** the user passes `--trace=false`
- **THEN** no trace file is written and pipeline behavior is identical to a run with tracing enabled

### Requirement: Trace File Location

The system SHALL write trace files to `$XDG_DATA_HOME/nelsonctl/traces/` by default, falling back to `~/.local/share/nelsonctl/traces/` when `XDG_DATA_HOME` is unset. The user MAY override this with `--trace-dir <path>`. The system SHALL create the trace directory and any parent directories if they do not exist.

#### Scenario: Default location with XDG

- **WHEN** `XDG_DATA_HOME` is set to `/home/user/.local/data` and no `--trace-dir` is provided
- **THEN** traces are written to `/home/user/.local/data/nelsonctl/traces/`

#### Scenario: Default location without XDG

- **WHEN** `XDG_DATA_HOME` is unset and no `--trace-dir` is provided
- **THEN** traces are written to `~/.local/share/nelsonctl/traces/`

#### Scenario: Custom location

- **WHEN** `--trace-dir /tmp/traces` is provided
- **THEN** traces are written to `/tmp/traces/`

#### Scenario: Directory creation

- **WHEN** the trace directory does not exist
- **THEN** the system creates it (with mode `0700`) before writing the first event

### Requirement: Trace File Permissions

The system SHALL create trace files with owner-only read/write permissions (`0600`) because prompts and agent output may contain sensitive data.

#### Scenario: File permissions

- **WHEN** a trace file is created
- **THEN** the file has mode `0600`

### Requirement: Trace File Naming

Each trace file SHALL be named `<timestamp>_<change-name>_<agent-name>.jsonl` where the timestamp is ISO 8601 basic format (`2006-01-02T150405Z`) at the start of the run, the change name is derived from the change directory, and the agent name is the selected agent.

#### Scenario: Opencode run

- **WHEN** running change `my-feature` with agent `opencode` starting at 2026-04-05T23:45:00Z
- **THEN** the trace file is named `2026-04-05T234500Z_my-feature_opencode.jsonl`

### Requirement: Run Meta Event

The system SHALL emit a `run_meta` event as the first line of every trace file containing the nelsonctl version, agent name, change name, hostname, and start time. The version SHALL be obtained from build-time injection via `-ldflags` or from `runtime/debug.ReadBuildInfo()`.

#### Scenario: Trace header

- **WHEN** a trace file is opened
- **THEN** the first line is a JSON object with `type: "run_meta"`, `version`, `agent`, `change`, `hostname`, and `started_at`

### Requirement: Run End Event

The system SHALL emit a `run_end` event as the last line of every trace file containing the final status (`completed`, `error`, or `interrupted`), total duration, phases completed, phases failed, and any error message. Context cancellation and SIGINT SHALL map to `interrupted`.

#### Scenario: Successful run

- **WHEN** the pipeline completes all phases and creates a PR
- **THEN** the last trace line has `type: "run_end"`, `status: "completed"`, and the summary counts

#### Scenario: Errored run

- **WHEN** the pipeline exits early due to an error
- **THEN** the last trace line has `type: "run_end"`, `status: "error"`, and the error message

#### Scenario: Interrupted run

- **WHEN** the user presses Ctrl+C or the context is cancelled
- **THEN** the last trace line has `type: "run_end"`, `status: "interrupted"`

### Requirement: Trace Event Enrichment

The system SHALL enrich pipeline event types with contextual data needed for traces. Enrichment SHALL be done by adding fields to pipeline event structs so that the TraceWriter can serialize events directly without a sidecar context. State events SHALL include the state name. Phase start events SHALL include the phase number, name, and attempt. Phase result events SHALL include the review output, pass/fail status, and attempt count. Agent invocation events SHALL include the prompt text, agent name, step, model, session ID, exit code, duration, and stdout/stderr lengths. Review result events SHALL include the full review output and the verdict. Commit events SHALL include the commit subject and changed files. Controller activity events SHALL include the tool name and any summary. Execution context events SHALL include the mode, agent, step, model, and resumed flag.

#### Scenario: Phase start event

- **WHEN** a phase begins execution
- **THEN** the trace contains `{"type":"phase_start","phase":1,"name":"...","attempt":1,"ts":"..."}`

#### Scenario: Agent invocation event

- **WHEN** an agent is invoked for a step
- **THEN** the trace contains `{"type":"agent_invoke","agent":"pi","step":"apply","model":"opencode-go/kimi-k2.5","session_id":"...","prompt":"...","work_dir":"...","ts":"..."}` followed by `{"type":"agent_result","exit_code":0,"duration_ms":45230,"stdout_len":8432,"stderr_len":0,"ts":"..."}`

#### Scenario: Review result event

- **WHEN** a review pass completes
- **THEN** the trace contains `{"type":"review_result","passed":true,"output":"no issues found","attempt":2,"step":"phase","phase":1,"ts":"..."}`

#### Scenario: Final review result event

- **WHEN** the final review pass completes
- **THEN** the trace contains `{"type":"review_result","passed":true,"output":"...","attempt":1,"step":"final","ts":"..."}`

#### Scenario: Output chunk event

- **WHEN** an agent invocation completes and produces stdout
- **THEN** the trace contains one `{"type":"output_chunk","chunk":"...","ts":"..."}` event with the full stdout content as a single chunk. Streaming per-token output chunks require the RPC integration and are not produced by CLI adapters.

#### Scenario: Commit event

- **WHEN** the pipeline commits a phase
- **THEN** the trace contains `{"type":"git_commit","subject":"feat(...): complete phase 1 - ...","files":["src/foo.go","src/bar.go"],"ts":"..."}`

#### Scenario: Artifact commit event

- **WHEN** the pipeline commits planning artifacts before the phase loop
- **THEN** the trace contains `{"type":"git_commit","subject":"chore: add litespec artifacts for ...","files":[...],"ts":"..."}`

#### Scenario: Branch reuse signal

- **WHEN** the pipeline reuses an existing branch
- **THEN** the `state_change` event for the `Branch` state includes `"branch_reused": true`

#### Scenario: Phase result event

- **WHEN** a phase finishes after all retry attempts
- **THEN** the trace contains `{"type":"phase_result","phase":1,"passed":true,"attempts":2,"review":"...","ts":"..."}`

#### Scenario: Taunt event

- **WHEN** a phase exhausts all retry attempts without passing review
- **THEN** the trace contains `{"type":"taunt","phase":1,"ts":"..."}`

#### Scenario: Git push event

- **WHEN** the pipeline pushes the branch to the remote
- **THEN** the trace contains `{"type":"git_push","remote":"origin","branch":"change/my-feature","ts":"..."}`

#### Scenario: PR created event

- **WHEN** the pipeline creates a pull request
- **THEN** the trace contains `{"type":"pr_created","title":"my-feature","url":"https://...","ts":"..."}`

#### Scenario: Summary event

- **WHEN** the pipeline emits the run summary
- **THEN** the trace contains `{"type":"summary","phases_completed":3,"phases_failed":1,"duration":"2m30s","branch":"change/my-feature","ts":"..."}`

### Requirement: Pi Adapter Event Capture

The system SHALL capture Pi adapter internals in traces by routing adapter events through the same fan-out point as pipeline events. The Pi adapter SHALL emit traceable events for: session creation (with session type and optional parent), session switching, model selection (with success/failure), RPC event summaries (agent_end with stop reason), stale event draining (with count), and crash recovery (with cause). These events SHALL flow through the agent's `Events()` channel into the trace writer. For non-Pi agents (CLI adapters like opencode, claude, codex, amp), adapter events SHALL NOT be emitted — the pipeline-level `agent_invoke` and `agent_result` events are sufficient.

#### Scenario: Pi adapter session created

- **WHEN** the Pi adapter creates a new implementation session
- **THEN** the trace contains `{"type":"session_created","session_id":"...","session_type":"impl","ts":"..."}`

#### Scenario: Pi adapter review session created

- **WHEN** the Pi adapter forks a review session from the implementation session
- **THEN** the trace contains `{"type":"session_created","session_id":"...","session_type":"review","parent_session":"/path/to/impl/session.json","ts":"..."}`

#### Scenario: Pi adapter session switched

- **WHEN** the Pi adapter switches the active session
- **THEN** the trace contains `{"type":"session_switched","session_id":"...","ts":"..."}`

#### Scenario: Pi adapter model set success

- **WHEN** the Pi adapter successfully sets the model
- **THEN** the trace contains `{"type":"model_set","provider":"opencode-go","model":"kimi-k2.5","success":true,"ts":"..."}`

#### Scenario: Pi adapter model set failure

- **WHEN** the Pi adapter fails to set the model
- **THEN** the trace contains `{"type":"model_set","provider":"minimax","model":"minimax-m2.7","success":false,"ts":"..."}`

#### Scenario: Pi adapter RPC event

- **WHEN** the Pi adapter receives an `agent_end` RPC event
- **THEN** the trace contains `{"type":"rpc_event","rpc_type":"agent_end","stop_reason":"toolUse","session_id":"...","ts":"..."}`

#### Scenario: Pi adapter events drained

- **WHEN** the Pi adapter drains stale events
- **THEN** the trace contains `{"type":"events_drained","count":4,"ts":"..."}`

#### Scenario: Pi adapter crash recovery

- **WHEN** the Pi adapter restarts after a crash
- **THEN** the trace contains `{"type":"agent_restarted","cause":"pi process exited unexpectedly","ts":"..."}`

#### Scenario: CLI adapter produces no adapter events

- **WHEN** nelsonctl runs with a CLI adapter (opencode, claude, etc.)
- **THEN** the trace contains `agent_invoke` and `agent_result` events but no `session_created`, `model_set`, `rpc_event`, or `events_drained` events

### Requirement: Trace Writer Error Isolation

The trace writer SHALL NOT propagate errors to the pipeline. If the trace writer encounters an I/O error (disk full, permission denied, write failure), it SHALL log the error to stderr, set an internal failed flag, and skip all subsequent writes for the remainder of the run. The pipeline SHALL continue executing normally regardless of trace writer state.

#### Scenario: Disk full during trace

- **WHEN** the trace file write fails due to a disk full condition
- **THEN** an error is logged to stderr, trace writing stops, and the pipeline continues to completion

#### Scenario: Channel overflow

- **WHEN** the background writer cannot keep up and the buffered channel is full
- **THEN** the event is dropped (not blocked) and the pipeline continues

### Requirement: Trace Flush on Exit

The system SHALL flush the trace file to disk before the process exits. Signal handling SHALL use a channel-based approach: SIGINT and SIGTERM are delivered to a goroutine via `os/signal.Notify`, which triggers a graceful close of the TraceWriter (flushing the buffer and writing the `run_end` event). Partial traces SHOULD be valid JSONL (every line is a complete JSON object) after a graceful shutdown. After an uncatchable signal (SIGKILL), the trace MAY be incomplete — this is an accepted limitation.

#### Scenario: SIGINT during phase

- **WHEN** the user presses Ctrl+C during an agent invocation
- **THEN** the signal-handling goroutine closes the TraceWriter, which flushes buffered events and writes a `run_end` event with `status: "interrupted"`

#### Scenario: No partial lines after graceful exit

- **WHEN** a trace file is inspected after a graceful shutdown
- **THEN** every line in the file is a valid JSON object ending with a newline

#### Scenario: SIGKILL accepted data loss

- **WHEN** the process is killed with SIGKILL
- **THEN** the trace file MAY be missing the final events and `run_end` line — this is documented and accepted

### Requirement: Trace Emission in All Execution Modes

The system SHALL emit trace events in both TUI mode and verbose mode. In verbose mode where no TUI channel is active, the `OnEvent` handler SHALL be set to dispatch directly to the TraceWriter.

#### Scenario: Verbose mode trace

- **WHEN** the user runs with `--verbose`
- **THEN** a complete trace file is produced with all events

#### Scenario: TUI mode trace

- **WHEN** the user runs in default TUI mode
- **THEN** a complete trace file is produced with all events, identical in content to a verbose mode trace of the same run
