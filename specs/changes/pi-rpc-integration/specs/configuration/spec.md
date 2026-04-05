# Configuration

## ADDED Requirements

### Requirement: Config File
The system SHALL load runtime configuration from `~/.config/nelsonctl/config.yaml`, including the effective agent, per-step models, per-step timeouts, controller provider/model settings, and the review failure threshold. Built-in defaults SHALL make Pi RPC usable even when the config file has not been created yet.

#### Scenario: Config present
- **WHEN** `config.yaml` defines agent, models, timeouts, controller, and review settings
- **THEN** nelsonctl uses those values for the run

#### Scenario: Config absent but Pi available
- **WHEN** no config file exists and Pi is installed
- **THEN** nelsonctl uses built-in defaults and runs in Pi mode

### Requirement: Initialization Wizard
The system SHALL provide `nelsonctl init` as an interactive setup wizard that creates or updates `~/.config/nelsonctl/config.yaml`. The wizard SHALL offer a minimal path with sane defaults and an advanced path that exposes agent selection, per-step models, per-step timeouts, controller settings, and review failure policy.

#### Scenario: Minimal setup
- **WHEN** the user chooses the minimal init flow
- **THEN** nelsonctl writes a working Pi-first config with default models, timeouts, and failure threshold

#### Scenario: Advanced setup
- **WHEN** the user chooses the advanced init flow
- **THEN** nelsonctl prompts for the configurable agent, model, timeout, controller, and review policy fields before writing the file

### Requirement: Credential Handling
The system MUST read provider credentials from environment variables and MUST NOT write secrets into `config.yaml`.

#### Scenario: Missing controller credential
- **WHEN** the resolved controller provider is enabled but its API key environment variable is missing
- **THEN** nelsonctl fails startup with a message naming the required environment variable

#### Scenario: Writing config
- **WHEN** `nelsonctl init` writes `config.yaml`
- **THEN** the file contains settings only and omits any API key values
