# run-tracing — Design Document

## Architecture

The trace writer is a passive event consumer that sits alongside the TUI in the event fan-out. It receives the same `interface{}` events as the TUI, serializes them to JSONL, and writes them to disk asynchronously.

```
pipeline.emit(msg)
     │
     ├──► OnEvent (existing) ──► TUI via toTeaMsg()
     │
     └──► TraceWriter.Send(msg) ──► buffered channel (cap 1024)
                                        │
                                        ▼ background goroutine
                                        json.Marshal → bufio.Writer → file

Pi adapter events:
agent.Events() channel ──► pipeline OnEvent ──► TraceWriter.Send(msg)
```

The `TraceWriter` is wired in `cli.go` in both the TUI path and the verbose path. In TUI mode, the pipeline's `OnEvent` is wrapped to fan out to both the TUI channel and the trace writer. In verbose mode, `OnEvent` dispatches only to the trace writer. No changes to the pipeline's event emission logic — the pipeline emits enriched event types that the TraceWriter serializes.

The Pi adapter (`internal/agent/pi.go`) emits events through its `Events()` channel for session lifecycle, model selection, and RPC event summaries. These events are routed into the pipeline's `OnEvent` handler alongside pipeline-level events, so the trace writer captures both without additional wiring.

## Decisions

### JSONL over SQLite

Chosen for simplicity, composability, and `jq`-friendliness. A single run produces one self-contained file. Cross-run comparison tooling can be built later. SQLite is deferred until the volume of traces justifies structured queries.

### Enriched event structs, not TraceContext sidecar

Rather than a shared `TraceContext` that the pipeline must actively update (creating coupling), the needed data (agent name, prompt text, duration, commit subjects) is added directly to pipeline event structs as new fields. The TraceWriter serializes whatever is on the event. This keeps the pipeline → TraceWriter data flow one-directional: the pipeline emits richer events, the writer consumes them. No sidecar, no `traceCtx.Update()` calls.

New pipeline event types added: `AgentInvokeEvent`, `AgentResultEvent`, `ReviewResultEvent`, `GitCommitEvent`, `GitPushEvent`, `PREvent`. Existing event types (`StateEvent`, `PhaseStartEvent`, `PhaseResultEvent`, `OutputEvent`, `TauntEvent`, `SummaryEvent`) get additional fields where needed (e.g. `branch_reused` on `StateEvent` for the Branch state). `AgentInvokeEvent` includes `Step` and `Model` fields to record which pipeline step triggered the invocation and what model was selected. `ExecutionContextEvent` and `ControllerActivityEvent` are already emitted by the pipeline and mapped in the TraceWriter — they are formalized in the event type registry.

### Async channel-based writer

`TraceWriter.Send(msg)` performs a non-blocking send to a buffered channel (capacity 1024). A background goroutine reads from the channel, marshals to JSON, and writes to a `bufio.Writer` (64KB buffer). If the channel is full, the event is dropped — the pipeline never blocks on trace I/O. This ensures the "passive observer" guarantee: trace write latency has zero impact on pipeline timing.

### Channel-based signal handling

SIGINT and SIGTERM are captured via `os/signal.Notify` into a channel. A dedicated goroutine waits on the signal channel and calls `TraceWriter.Close()`, which flushes the bufio buffer, writes the `run_end` event, and syncs the file. No I/O happens in the signal handler itself — only channel sends. This avoids the well-known deadlock risk of performing file I/O inside a signal handler.

SIGKILL cannot be caught. The bufio buffer may be lost. This is documented and accepted. The async writer could optionally `file.Sync()` every N events as a tunable trade-off, but the default prioritizes performance.

### XDG-compliant trace directory

Default trace directory follows the XDG Base Directory Specification: `$XDG_DATA_HOME/nelsonctl/traces/`, falling back to `~/.local/share/nelsonctl/traces/`. The `--trace-dir` flag overrides this. The directory is created with mode `0700`; trace files are created with mode `0600` because they may contain prompts and agent output with sensitive data.

### Wrapped OnEvent for both execution modes

In TUI mode, `cli.go` wraps the existing `toTeaMsg` closure and the `TraceWriter.Send` into a single fan-out closure assigned to `p.OnEvent`. In verbose mode, `p.OnEvent` is set to `TraceWriter.Send` directly (no TUI channel). This ensures traces are emitted in both modes without duplicating the fan-out logic.

### Error isolation

The background writer goroutine catches I/O errors, logs them to stderr, and sets a `failed` flag. Once failed, subsequent events are silently dropped. The pipeline is never affected — `Send()` is always a non-blocking channel send regardless of writer state.

### Package dependency: trace imports pipeline

The background goroutine in `TraceWriter` performs a type switch on pipeline event structs to map them to trace event structs. This creates an `internal/trace` → `internal/pipeline` import. This is acceptable because the dependency is one-directional (trace reads pipeline types, pipeline does not import trace) and the alternative — having pipeline events implement a `ToTraceEvent()` interface — would couple pipeline to trace in the other direction. The type-switch mapping lives entirely in `internal/trace/writer.go`.

### Pi adapter events as new typed structs via Event extension

The Phase 2.5 tasks define new adapter event types (`SessionCreatedEvent`, `SessionSwitchedEvent`, etc.) as distinct structs in `internal/agent/adapter.go`. These are NOT the existing `Event` struct with `Metadata` map fields — they are separate typed structs.

However, the existing `Events()` channel has type `<-chan Event`. Rather than changing this to `<-chan interface{}` (a breaking change to the `Agent` interface), the adapter events are sent through the existing channel by adding a `TracePayload interface{}` field to the `Event` struct. The Pi adapter sets `TracePayload` to the typed adapter event struct when emitting a trace-specific event, and leaves it `nil` for regular output events (`TextEvent`, `CompletionEvent`, `ErrorEvent`).

The TraceWriter's type switch in `toTraceEvent()` checks `TracePayload` first. If it's non-nil and matches a known adapter type, it maps directly. This avoids the `trace → agent` import (the TraceWriter doesn't need to import agent types — `cli.go` can pre-convert `TracePayload` to a trace-friendly type before sending to the TraceWriter).

Actually, the simplest approach that keeps all mapping in one place: the TraceWriter imports `internal/agent` alongside `internal/pipeline` and the `toTraceEvent()` type switch handles both pipeline types and adapter types. The `TracePayload` field carries the adapter event struct through the channel, and the type switch recognizes it. This follows the same pattern as the existing pipeline type switch.

For non-Pi agents (CLI adapters like opencode, claude, codex, amp), the agent's `Events()` channel is not used for adapter events. The pipeline-level `AgentInvokeEvent` and `AgentResultEvent` are sufficient — they wrap the CLI subprocess call.

### Apply run-tracing before pi-rpc-integration

This change should be applied before `pi-rpc-integration`. Both changes modify `pipeline.go`, but run-tracing's modifications are purely additive (new `p.emit()` calls around existing operations). Pi-rpc-integration restructures the phase execution loop (mode selection, RPC sessions, resume logic). Applying run-tracing first gives pi-rpc-integration a clear set of event emission points to preserve when it restructures the loop. The pi-rpc spec should treat the trace events as a stable contract that must be maintained.

### Import direction: trace → pipeline

The `TraceWriter` performs a type switch on pipeline event structs to map them to trace event types. This means `internal/trace` imports `internal/pipeline` (for the event struct types). The reverse dependency (pipeline importing trace) is avoided — pipeline event structs are defined in the pipeline package and carry their own enrichment data. This is a deliberate unidirectional dependency: pipeline owns the event types, trace consumes them. If future needs require pipeline to be aware of trace, a `TraceableEvent` interface defined in the trace package and satisfied by pipeline events would be the pattern — but that is not needed here.

## File Changes

- `internal/trace/events.go` — typed trace event structs with JSON tags for all event types in the registry, including Pi adapter events (`SessionCreatedTraceEvent`, `SessionSwitchedTraceEvent`, `ModelSetTraceEvent`, `RPCRawTraceEvent`, `EventsDrainedTraceEvent`, `AgentRestartedTraceEvent`), execution context events, and controller activity events. Supports `Event Type Registry`.
- `internal/trace/writer.go` — `TraceWriter` struct: `New(path string, meta RunMetaEvent)` (creates file with mode `0600`, writes `run_meta` as the first line, starts the background goroutine), `Send(msg interface{})` (non-blocking channel send), `Close(runEnd RunEndEvent)` (flush + run_end), background goroutine, error isolation. Type switch extended to handle Pi adapter event types from `internal/agent/`. Supports `JSONL Trace Emission`, `Trace Writer Error Isolation`, `Trace Flush on Exit`, and `Pi Adapter Event Capture`.
- `internal/agent/pi.go` — emit structured adapter events through `Events()` channel: `SessionCreatedEvent` in `StartImplementationSession` and `StartReviewSession`, `SessionSwitchedEvent` in `switchToSession`, `ModelSetEvent` in `SendMessage` (after `set_model` RPC), `RPCRawEvent` in `forwardEvents` for `agent_end` events, `EventsDrainedEvent` in `consumeSessionEvents` (with drain count), `AgentRestartedEvent` in `restartAfterCrash`. Supports `Pi Adapter Event Capture`.
- `internal/agent/adapter.go` — define adapter event types (`SessionCreatedEvent`, `SessionSwitchedEvent`, `ModelSetEvent`, `RPCRawEvent`, `EventsDrainedEvent`, `AgentRestartedEvent`) as typed structs with JSON-tagged fields. These flow through the existing `Events()` channel via the `Event` wrapper. Supports `Pi Adapter Event Capture`.
- `internal/pipeline/events.go` — new pipeline event types: `AgentInvokeEvent{Agent, Step, Model, SessionID, Prompt, WorkDir}`, `AgentResultEvent{ExitCode, DurationMs, StdoutLen, StderrLen}`, `ReviewResultEvent{Passed, Output, Attempt, Step, Phase}` (JSON tag `phase` uses `omitempty` — always set when `Step == "phase"`, zero-value/omitted when `Step == "final"`), `GitCommitEvent{Subject, Files}`, `GitPushEvent{Remote, Branch}`, `PREvent{Title, URL}`. Supports `Trace Event Enrichment`.
- `internal/pipeline/pipeline.go` — emit new event types around `Agent.ExecuteStep()` calls (apply, review, fix, final review), `Git.Add/Commit`, `Git.Push`, and `PR.Create`. Add `branch_reused` field to `StateEvent` emission for the Branch state. Emit `ReviewResultEvent` in the final review loop. No changes to retry logic, commit behavior, or phase progression.
- `cmd/nelsonctl/cli.go` — add `--trace` and `--trace-dir` flags, resolve XDG path, create `TraceWriter` with run metadata (version, agent, change, hostname), wire fan-out `OnEvent` for both TUI and verbose paths (including draining agent `Events()` channel for Pi adapter events), defer `TraceWriter.Close()` after `p.Run()` for normal completion, set up signal-handling goroutine for SIGINT/SIGTERM. Supports `Trace File Location`, `Trace File Permissions`, `Trace Emission in All Execution Modes`, `Pi Adapter Event Capture`.
- `internal/trace/writer_test.go` — unit tests for JSON serialization, enrichment, error isolation, channel overflow behavior, and partial trace validity.
- `cmd/nelsonctl/cli_test.go` — update integration tests to verify trace files are produced in both TUI and verbose modes and contain expected events.
