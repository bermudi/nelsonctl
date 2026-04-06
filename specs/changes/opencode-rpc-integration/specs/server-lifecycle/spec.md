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
The adapter SHALL support connecting to an already-running opencode server by accepting a base URL via the configuration system established by `pi-rpc-integration` (`opencode_server_url` in `config.yaml`), skipping process startup and health check, and deferring shutdown to the external owner.

#### Scenario: External server configured
- **WHEN** `opencode_server_url` is set in the pi-rpc configuration
- **THEN** nelsonctl connects to that server without starting a new process and does not shut it down on exit

#### Scenario: No external server configured
- **WHEN** `opencode_server_url` is not set in the configuration
- **THEN** nelsonctl starts and manages its own server process

#### Scenario: Server command override for debugging
- **WHEN** the operator wants visual debugging via a browser
- **THEN** the server manager can be configured to use `opencode web` instead of `opencode serve`, which starts the same HTTP server with an additional web interface
