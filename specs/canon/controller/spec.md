# Controller

## Requirements

### Requirement: Controller Agent Loop

The system SHALL implement a controller AI that drives each implementation phase as a tool-calling agent loop. The controller MUST run as one conversation per phase using DeepSeek 3.2 reasoning (or a configured alternative model). The conversation MUST be discarded after the phase completes, and each new phase MUST start with a fresh conversation. The controller MUST work in both Nelson mode (Pi RPC) and Ralph mode (CLI agents).

#### Scenario: Phase controller lifecycle

- **WHEN** the pipeline begins phase N
- **THEN** it creates a fresh controller conversation with the phase tasks and review.fail_on threshold in the system prompt, runs the controller's tool-calling loop until the controller calls `approve` or the pipeline enforces termination, and discards the conversation

#### Scenario: Controller works with CLI agent

- **WHEN** the effective agent is a CLI adapter and the controller calls `submit_prompt`
- **THEN** the pipeline routes the prompt to the CLI agent via shell-out and returns the completion status to the controller

#### Scenario: Controller works with Pi agent

- **WHEN** the effective agent is Pi and the controller calls `submit_prompt`
- **THEN** the pipeline routes the prompt to the Pi RPC session and returns the completion status to the controller

### Requirement: Controller Tools

The controller MUST have exactly five tools available. No other tools SHALL be provided.

- `read_file(path: string) → string` — reads a file from the workspace and returns its contents. Used to discover specs, design documents, code, and other artifacts.
- `get_diff() → string` — returns the current `git diff` output showing uncommitted changes in the workspace.
- `submit_prompt(prompt: string) → string` — sends a prompt to the implementation agent (Pi or CLI) and blocks until the agent completes. Returns "Agent completed successfully." on success or an error description on failure. Does NOT return the agent's full transcript.
- `run_review() → string` — triggers a mechanical litespec-review using the configured review prompt and returns the raw review output. The review prompt is NOT drafted by the controller.
- `approve(summary: string)` — declares the phase passed with a one-line summary. The pipeline commits the phase and moves on. This tool ends the controller conversation.

#### Scenario: Controller reads spec before drafting prompt

- **WHEN** the controller needs to understand the requirements for the current phase
- **THEN** it calls `read_file` with the path to the relevant component spec

#### Scenario: Controller inspects changes after fix

- **WHEN** the controller has sent a fix prompt and wants to see what the agent changed
- **THEN** it calls `get_diff` to inspect the current workspace changes

#### Scenario: Controller triggers review

- **WHEN** the controller has sent an apply or fix prompt and the agent has completed
- **THEN** it calls `run_review` to get an independent assessment

#### Scenario: Controller approves phase

- **WHEN** the controller determines the review output meets the configured quality threshold
- **THEN** it calls `approve` with a short summary, ending the conversation

### Requirement: Controller Prompt Drafting

The controller MUST draft the initial apply prompt for each phase by reading the relevant specs and design documents via `read_file`. The resulting prompt MUST be richer than the scaffold's hardcoded template — it SHALL include design rationale, relevant spec scenarios, and file change targets discovered from the artifacts.

#### Scenario: Rich initial prompt

- **WHEN** the controller begins a new phase
- **THEN** it reads the proposal, design, and relevant component spec via `read_file`, then calls `submit_prompt` with a prompt that references specific requirements, design decisions, and target files

#### Scenario: Missing artifact file

- **WHEN** the controller calls `read_file` for a spec or design file that does not exist
- **THEN** the controller proceeds with available context, using whatever artifacts it could discover

### Requirement: Controller Review Analysis

The controller MUST analyze review output through comprehension, not parsing. When `run_review` returns the raw review output, the controller reads and reasons about it in the context of the spec requirements and the `review.fail_on` threshold injected in its system prompt. It MUST NOT rely on regex patterns, substring matching, or structured format expectations.

#### Scenario: Review with only warnings when fail_on is critical

- **WHEN** the review output contains warnings but no critical issues and `review.fail_on` is `critical`
- **THEN** the controller calls `approve` because the findings are below the configured threshold

#### Scenario: Review with ambiguous output

- **WHEN** the review output does not follow the expected litespec-review format
- **THEN** the controller still reasons about the content and makes a pass/fail decision through comprehension

### Requirement: Controller Fix Steering

The controller MUST craft targeted fix prompts when a review fails. The fix prompt MUST be informed by the review output, the spec requirements, and the current diff. The controller SHOULD reference specific issues, suggest approaches, and avoid repeating strategies that failed in previous attempts within the same conversation.

#### Scenario: Targeted fix after first failure

- **WHEN** the review identifies specific issues
- **THEN** the controller crafts a fix prompt that references the issues, relevant spec requirements, and suggests a concrete fix approach

#### Scenario: Adjusted strategy on second attempt

- **WHEN** the first fix attempt did not resolve the issue
- **THEN** the controller crafts a different fix prompt, informed by the conversation history showing what was already tried

### Requirement: Controller System Prompt

The controller's system prompt MUST include the current phase tasks and the `review.fail_on` threshold. It MUST describe the controller's role, available tools, and constraints. It MUST NOT include the full proposal, design, or specs — those are discovered via `read_file` on demand.

#### Scenario: System prompt contents

- **WHEN** the pipeline creates a controller conversation for phase 2
- **THEN** the system prompt includes the phase 2 task list, the review.fail_on value, the tool descriptions, and the controller's role description

### Requirement: Final Review Controller

The system SHALL use a fresh controller conversation to handle the final pre-archive review. The controller MUST follow the same tool-calling pattern as phase reviews but with a system prompt scoped to the full change rather than a single phase.

#### Scenario: Final review loop

- **WHEN** all phases are complete
- **THEN** the pipeline creates a final-review controller conversation that calls `run_review`, analyzes the output, and either calls `approve` or crafts fix prompts

### Requirement: Controller Retry Budget

The pipeline MUST enforce a maximum of 3 fix-review cycles per phase. After each failed review, the pipeline injects the attempt count ("Attempt N of 3") into the controller conversation so the controller can adjust strategy on the final attempt. When the third fix-review cycle still fails, the pipeline MUST terminate the controller conversation and mark the phase as failed.

#### Scenario: Third attempt fails

- **WHEN** the controller has completed 3 fix-review cycles without calling `approve`
- **THEN** the pipeline terminates the conversation, preserves on-disk work, and marks the phase as failed

#### Scenario: Attempt count injected

- **WHEN** a review fails and the controller is about to send a fix prompt
- **THEN** the pipeline injects "Attempt N of 3" into the controller conversation before the controller's next turn

### Requirement: Controller Guardrails

The pipeline MUST enforce a maximum of 50 tool calls and a 45-minute timeout per controller conversation. These limits MUST be configurable. When either limit is reached, the pipeline MUST terminate the controller conversation and mark the phase as failed.

#### Scenario: Tool call budget exceeded

- **WHEN** the controller has made 50 tool calls without calling `approve`
- **THEN** the pipeline terminates the conversation and marks the phase as failed

#### Scenario: Timeout exceeded

- **WHEN** the controller conversation has been running for 45 minutes
- **THEN** the pipeline terminates the conversation and marks the phase as failed

### Requirement: Controller Failure Recovery

The system MUST retry controller API calls 3 times with exponential backoff when the provider is unreachable. If all retries fail, the pipeline MUST fail cleanly and preserve resumable state. The existing resume mechanism (first unchecked task in `tasks.md`) handles recovery on relaunch.

#### Scenario: Transient API failure

- **WHEN** a controller API call fails due to a network error
- **THEN** the pipeline retries up to 3 times with exponential backoff before failing

#### Scenario: Persistent API failure

- **WHEN** all 3 retries fail
- **THEN** the pipeline fails the run cleanly, preserving progress for resumable relaunch

### Requirement: Controller Provider Configuration

The system MUST support two controller providers: DeepSeek direct API and OpenRouter. Both use OpenAI-compatible `/chat/completions` endpoints with tool calling. The provider and model MUST be configurable in `config.yaml` under the `controller` section. Credentials MUST come from environment variables.

#### Scenario: DeepSeek direct

- **WHEN** `controller.provider` is `deepseek` and `DEEPSEEK_API_KEY` is set
- **THEN** the controller calls the DeepSeek API directly

#### Scenario: OpenRouter provider

- **WHEN** `controller.provider` is `openrouter` and `OPENROUTER_API_KEY` is set
- **THEN** the controller calls the OpenRouter API with the configured model

#### Scenario: Missing credential

- **WHEN** the controller provider is configured but the corresponding API key environment variable is missing
- **THEN** the pipeline fails startup with a message naming the required variable
