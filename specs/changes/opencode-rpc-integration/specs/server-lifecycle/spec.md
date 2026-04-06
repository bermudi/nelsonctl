# Server Lifecycle

## ADDED Requirements

### Requirement: Managed Server Process
The opencode RPC adapter SHALL start `opencode serve` as a managed child process with a dynamically assigned port (`--port 0`) and SHALL wait for the server to report healthy via `GET /global/health` before accepting pipeline work. The adapter MUST shut down the server process when the pipeline completes, aborts, or encounters an unrecoverable error.

#### Scenario: Server starts and becomes healthy
- **WHEN** nelsonctl selects opencode as the RPC agent and starts a pipeline run
- **THEN** the adapter launches `opencode serve --port 0`, reads the assigned port from stdout or probes until `GET /global/health` returns `{ healthy: true }`, and proceeds with pipeline work

#### Scenario: Server fails to start
- **WHEN** `opencode serve` exits with a non-zero code during startup or does not become healthy within a configurable timeout
- **THEN** nelsonctl reports a startup error with the server's stderr output and does not proceed with the pipeline

#### Scenario: Server crashes mid-run
- **WHEN** the opencode server process exits unexpectedly during pipeline execution
- **THEN** the adapter initiates crash recovery as specified in the `Crash Recovery` spec

#### Scenario: Clean shutdown
- **WHEN** the pipeline finishes, aborts, or encounters an error that stops the run
- **THEN** the adapter calls `POST /instance/dispose` and terminates the server process with a grace period before SIGKILL

### Requirement: Server Reuse
The adapter SHALL support connecting to an already-running opencode server by accepting a base URL via the `OPENCODE_SERVER_URL` environment variable (takes precedence) or the `opencode.server_url` key in pi-rpc-integration's `config.yaml`. When an external URL is provided, the adapter skips process startup and defers shutdown to the external owner.

#### Scenario: External server configured
- **WHEN** `OPENCODE_SERVER_URL` is set or `opencode.server_url` is present in `config.yaml`
- **THEN** nelsonctl connects to that server without starting a new process and does not shut it down on exit

#### Scenario: No external server configured
- **WHEN** neither `OPENCODE_SERVER_URL` nor `opencode.server_url` is set
- **THEN** nelsonctl starts and manages its own server process

#### Scenario: Pipeline abort with external server
- **WHEN** the pipeline aborts while connected to an externally managed server
- **THEN** the adapter closes the SSE connection and releases HTTP client resources but does NOT call `POST /instance/dispose` or terminate the server process

### Requirement: Server Command Override
The adapter SHALL support overriding the server subcommand via the `OPENCODE_SERVER_COMMAND` environment variable. When set to `web`, the adapter launches `opencode web` instead of `opencode serve`, which starts the same HTTP server with an additional browser-based interface for debugging. The default command is `serve`.

#### Scenario: Server command override for debugging
- **WHEN** `OPENCODE_SERVER_COMMAND=web` is set in the environment
- **THEN** the server manager launches `opencode web --port 0` instead of `opencode serve --port 0`

#### Scenario: Default server command
- **WHEN** `OPENCODE_SERVER_COMMAND` is not set
- **THEN** the server manager launches `opencode serve --port 0`
