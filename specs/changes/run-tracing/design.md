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
```

The `TraceWriter` is wired in `cli.go` in both the TUI path and the verbose path. In TUI mode, the pipeline's `OnEvent` is wrapped to fan out to both the TUI channel and the trace writer. In verbose mode, `OnEvent` dispatches only to the trace writer. No changes to the pipeline's event emission logic — the pipeline emits enriched event types that the TraceWriter serializes.

## Decisions

### JSONL over SQLite

Chosen for simplicity, composability, and `jq`-friendliness. A single run produces one self-contained file. Cross-run comparison tooling can be built later. SQLite is deferred until the volume of traces justifies structured queries.

### Enriched event structs, not TraceContext sidecar

Rather than a shared `TraceContext` that the pipeline must actively update (creating coupling), the needed data (agent name, prompt text, duration, commit subjects) is added directly to pipeline event structs as new fields. The TraceWriter serializes whatever is on the event. This keeps the pipeline → TraceWriter data flow one-directional: the pipeline emits richer events, the writer consumes them. No sidecar, no `traceCtx.Update()` calls.

New pipeline event types added: `AgentInvokeEvent`, `AgentResultEvent`, `ReviewResultEvent`, `GitCommitEvent`, `GitPushEvent`, `PREvent`. Existing event types (`StateEvent`, `PhaseStartEvent`, `PhaseResultEvent`, `OutputEvent`, `TauntEvent`, `SummaryEvent`) get additional fields where needed (e.g. `branch_reused` on `StateEvent` for the Branch state).

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

### Apply run-tracing before pi-rpc-integration

This change should be applied before `pi-rpc-integration`. Both changes modify `pipeline.go`, but run-tracing's modifications are purely additive (new `p.emit()` calls around existing operations). Pi-rpc-integration restructures the phase execution loop (mode selection, RPC sessions, resume logic). Applying run-tracing first gives pi-rpc-integration a clear set of event emission points to preserve when it restructures the loop. The pi-rpc spec should treat the trace events as a stable contract that must be maintained.

### Import direction: trace → pipeline

The `TraceWriter` performs a type switch on pipeline event structs to map them to trace event types. This means `internal/trace` imports `internal/pipeline` (for the event struct types). The reverse dependency (pipeline importing trace) is avoided — pipeline event structs are defined in the pipeline package and carry their own enrichment data. This is a deliberate unidirectional dependency: pipeline owns the event types, trace consumes them. If future needs require pipeline to be aware of trace, a `TraceableEvent` interface defined in the trace package and satisfied by pipeline events would be the pattern — but that is not needed here.

## File Changes

- `internal/trace/events.go` — typed trace event structs with JSON tags for all 14 event types in the registry. Supports `Event Type Registry`.
- `internal/trace/writer.go` — `TraceWriter` struct: `New(path string, meta RunMetaEvent)` (creates file with mode `0600`, writes `run_meta` as the first line, starts the background goroutine), `Send(msg interface{})` (non-blocking channel send), `Close(runEnd RunEndEvent)` (flush + run_end), background goroutine, error isolation. Supports `JSONL Trace Emission`, `Trace Writer Error Isolation`, and `Trace Flush on Exit`.
- `internal/version/version.go` — `Version` variable set via `-ldflags` at build time, with `runtime/debug.ReadBuildInfo()` fallback. Supports `Run Meta Event`.
- `internal/pipeline/events.go` — new pipeline event types: `AgentInvokeEvent{Agent, Prompt, WorkDir}`, `AgentResultEvent{ExitCode, DurationMs, StdoutLen, StderrLen}`, `ReviewResultEvent{Passed, Output, Attempt, Step, Phase}` (JSON tag `phase` uses `omitempty` — always set when `Step == "phase"`, zero-value/omitted when `Step == "final"`), `GitCommitEvent{Subject, Files}`, `GitPushEvent{Remote, Branch}`, `PREvent{Title, URL}`. Supports `Trace Event Enrichment`.
- `internal/pipeline/pipeline.go` — emit new event types around `Agent.Run()` calls (apply, review, fix, final review), `Git.Add/Commit`, `Git.Push`, and `PR.Create`. Add `branch_reused` field to `StateEvent` emission for the Branch state. Emit `ReviewResultEvent` in the final review loop. No changes to retry logic, commit behavior, or phase progression.
- `cmd/nelsonctl/cli.go` — add `--trace` and `--trace-dir` flags, resolve XDG path, create `TraceWriter` with run metadata (version, agent, change, hostname), wire fan-out `OnEvent` for both TUI and verbose paths, defer `TraceWriter.Close()` after `p.Run()` for normal completion, set up signal-handling goroutine for SIGINT/SIGTERM. Supports `Trace File Location`, `Trace File Permissions`, `Trace Emission in All Execution Modes`.
- `internal/trace/writer_test.go` — unit tests for JSON serialization, enrichment, error isolation, channel overflow behavior, and partial trace validity.
- `cmd/nelsonctl/cli_test.go` — update integration tests to verify trace files are produced in both TUI and verbose modes and contain expected events.
