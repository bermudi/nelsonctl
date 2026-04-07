# Streaming and Events

## ADDED Requirements

### Requirement: SSE Event Consumption
The adapter SHALL subscribe to `GET /global/event` (SSE) for the lifetime of the server connection and SHALL parse incoming events into typed pipeline messages that the TUI can consume.

#### Scenario: Streaming apply output
- **WHEN** the implementation session is processing an apply prompt
- **THEN** the adapter forwards text-delta events as `Event{Type: TextEvent, Content: delta}` values to the `Events()` channel

#### Scenario: Tool execution events
- **WHEN** opencode emits tool execution events during a step
- **THEN** the adapter emits `Event{Type: TextEvent, Content: "[tool] <tool_name> <status>"}` to the `Events()` channel

#### Scenario: Server error event
- **WHEN** the opencode server returns an error or the SSE stream reports an error
- **THEN** the adapter emits `Event{Type: ErrorEvent, Content: err.Error()}` to the `Events()` channel

#### Scenario: Step completion event
- **WHEN** a synchronous message call completes and the adapter has extracted the response
- **THEN** the adapter emits `Event{Type: CompletionEvent}` to the `Events()` channel to signal step boundary

### Requirement: Event Coalescing
The adapter SHALL coalesce rapid SSE events onto a 50ms render tick (configurable via `opencode.event_coalesce_ms` in `config.yaml`, default: 50) before forwarding them to the TUI via the `Events()` channel, so that bursty model output does not block the Bubble Tea render loop. The full text MUST be preserved in memory even when batches are coalesced for rendering.

#### Scenario: Burst of text deltas
- **WHEN** the model emits many text-delta events in rapid succession
- **THEN** the adapter batches them into a single output update per render tick while preserving the full text in memory

#### Scenario: Render tick configuration
- **WHEN** `opencode.event_coalesce_ms` is set in `config.yaml`
- **THEN** the adapter uses that value as the coalescing interval instead of the 50ms default

### Requirement: Response Extraction
After a synchronous `POST /session/:id/message` call completes, the adapter SHALL return the assistant's response text as the `Result.Stdout` field, preserving compatibility with the existing pipeline's `resultText()` helper.

#### Scenario: Apply completes
- **WHEN** the opencode message endpoint returns the assistant response
- **THEN** the adapter extracts the text content from the response parts and returns it as `Result.Stdout`

#### Scenario: Review completes
- **WHEN** the review message endpoint returns the assistant response
- **THEN** the adapter extracts the full review text and returns it as `Result.Stdout` for the controller AI to analyze
