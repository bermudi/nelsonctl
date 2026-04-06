# Streaming and Events

## ADDED Requirements

### Requirement: SSE Event Consumption
The adapter SHALL subscribe to `GET /global/event` (SSE) for the lifetime of the server connection and SHALL parse incoming events into typed pipeline messages that the TUI and review parser can consume.

#### Scenario: Streaming apply output
- **WHEN** the implementation session is processing an apply prompt
- **THEN** the adapter forwards text-delta events as `OutputEvent` messages to the TUI and accumulates the full response for review parsing

#### Scenario: Tool execution events
- **WHEN** opencode emits tool execution events during a step
- **THEN** the adapter surfaces tool names and statuses in the TUI output panel

### Requirement: Event Coalescing
The adapter SHALL coalesce rapid SSE events onto a short render tick before forwarding them to the TUI, so that bursty model output does not block the Bubble Tea render loop.

#### Scenario: Burst of text deltas
- **WHEN** the model emits many text-delta events in rapid succession
- **THEN** the adapter batches them into a single output update per render tick while preserving the full text in memory

### Requirement: Response Extraction
After a synchronous `POST /session/:id/message` call completes, the adapter SHALL return the assistant's response text as the `Result.Stdout` field, preserving compatibility with the existing pipeline's `resultText()` helper and review parser.

#### Scenario: Apply completes
- **WHEN** the opencode message endpoint returns the assistant response
- **THEN** the adapter extracts the text content from the response parts and returns it as `Result.Stdout`

#### Scenario: Review completes
- **WHEN** the review message endpoint returns the assistant response
- **THEN** the adapter extracts the full review text for the three-tier review parser
