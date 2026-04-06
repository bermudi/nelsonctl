# run-tracing

## Motivation

nelsonctl orchestrates multi-phase agent runs but has no persistent record of what happened. If a run fails, the only diagnostic is whatever was on screen. If you want to compare `opencode` against `claude` on the same change, you have to manually time each one and eyeball the output. There is no structured data to query, diff, or build tooling on top of.

The existing event system in `pipeline.emit()` already fires typed events for every state transition, phase attempt, review result, and summary. That data is currently consumed by the TUI and then discarded. Writing it to disk as JSONL is a small, self-contained change that unlocks debugging, benchmarking, and cross-run comparison without requiring the RPC integration.

## Scope

- Add a JSONL trace writer that hooks into the pipeline event system and appends one JSON object per event to a trace file on disk.
- Each trace file records one complete run: state transitions, phase attempts, prompts sent, agent results (exit code, duration, stdout/stderr lengths), review verdicts, git commits, and the final summary.
- Trace files are written to `$XDG_DATA_HOME/nelsonctl/traces/` (defaulting to `~/.local/share/nelsonctl/traces/` when `XDG_DATA_HOME` is unset) with a filename that encodes the timestamp, change name, and agent name so runs are easy to find and compare.
- Add a `--trace` flag (default on) and a `--trace-dir` flag to control trace output location.
- Enrich pipeline event types with the data needed for useful traces: prompt text, agent name, result durations, commit SHAs, and review details. Add new pipeline event types for agent invocations, results, and git operations so the TraceWriter can consume them without a sidecar context.
- Emit a structured `RunMeta` event at the start of every trace with the nelsonctl version, agent name, change name, and hostname for identification.
- Emit a structured `RunEnd` event at the close of every trace with pass/fail counts, total duration, and the final state.
- Trace files are created with owner-only permissions (`0600`) because prompts and agent output may contain sensitive data (file paths, task descriptions, code snippets, credentials echoed by agents). Operators should treat trace files as potentially secret-bearing artifacts.

## Non-Goals

- Not capturing agent internals (tool calls, thinking tokens, streaming deltas). That data is not available from CLI adapters and requires the Pi RPC integration planned in `pi-rpc-integration`. The trace schema is designed to accept those events when they become available.
- Not building a comparison or benchmarking UI. The trace files are the foundation; a `nelsonctl trace compare` or dashboard is a separate change.
- Not adding SQLite, a database, or an external service. JSONL files are the only storage format.
- Not changing the pipeline orchestration logic or retry behavior. New pipeline event types are added for trace-specific data (agent invocations, results, git operations), and existing event types are enriched with additional fields. The TUI and other consumers are unaffected by the new fields.
- Not sending traces over the network. Local files only.
- Not implementing trace file cleanup, rotation, or size management. A future change may add `nelsonctl trace clean` or automatic retention limits.
