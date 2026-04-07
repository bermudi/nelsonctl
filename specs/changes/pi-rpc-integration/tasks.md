# Tasks

## Phase 1: Runtime Configuration
- [x] Add config structs, defaults, XDG loading, and validation for `Config File`, including controller provider/model settings
- [x] Implement `nelsonctl init` minimal and advanced flows for `Initialization Wizard`, including controller configuration
- [x] Wire startup checks for `Agent Prerequisite Check`, `Workspace Validation`, and `Credential Handling` (including controller provider credentials)
- [x] Implement execution-mode resolution for `Pi-First Agent Resolution`
- [x] Write tests for config parsing, environment-driven credentials, controller settings, and startup validation

## Phase 2: Controller AI
- [x] Define the Controller interface and tool types in `internal/controller/controller.go` — `read_file`, `get_diff`, `submit_prompt`, `run_review`, `approve`
- [x] Implement the DeepSeek/OpenRouter client using OpenAI-compatible `/chat/completions` with tool calling in `internal/controller/controller.go`
- [x] Implement the tool-calling agent loop: dispatch tool calls, collect results, feed back into conversation, enforce 50-call and 45-minute guardrails
- [x] Write system prompts for phase controller and final-review controller in `internal/controller/prompts.go`
- [x] Implement tool dispatch in `internal/controller/tools.go` — `read_file` reads from disk, `get_diff` shells to git, `submit_prompt` and `run_review` delegate to the pipeline's agent execution, `approve` signals phase completion
- [x] Implement retry with exponential backoff (3 attempts) for controller API failures
- [x] Write tests for tool dispatch, agent loop lifecycle, guardrail enforcement, and API failure recovery using a mock OpenAI-compatible server

## Phase 3: Pi RPC Transport
- [x] Extend the agent contract to support both CLI and RPC implementations for `Two-Tier Agent Execution`
- [x] Implement Pi process startup, JSONL framing, request correlation, and typed RPC payloads for `Pi RPC Sessions`
- [x] Implement apply-session reuse, disposable review sessions, and per-step model switching for `Pi RPC Sessions`
- [x] Auto-discover workspace skills, disable extensions, and auto-cancel `extension_ui_request` events during Pi runs
- [x] Implement Pi crash restart behavior for `Pi Crash Recovery`
- [x] Write tests for RPC framing, session lifecycle, and crash recovery

## Phase 4: Pipeline Integration
- [x] Restructure the pipeline orchestrator to create a controller conversation per phase and delegate all prompt crafting and review analysis to the controller
- [x] Implement `submit_prompt` and `run_review` tool handlers that route to the effective agent (Pi RPC or CLI shell-out)
- [x] Remove `ReviewPassed()` regex detection, heuristic substring matching, and hardcoded `ApplyPrompt`/`FixPrompt`/`ReviewPrompt` templates
- [x] Implement the mechanical review prompt as a fixed template used by `run_review` tool handler
- [x] Inject `review.fail_on` threshold and phase tasks into the controller's system prompt
- [x] Inject attempt count ("Attempt N of M") into the controller conversation after each failed review
- [x] Implement branch reuse, first-unchecked-phase resume, and recovery commits for `Resumable Change Branch`
- [x] Add `.nelsonctl.lock` creation, stale PID cleanup, and lock release for `Run Locking`
- [x] Stage only changed files via `git diff --name-only` for `Recovery Commits and Scoped Staging`
- [x] Implement the dry-run planner for `Dry-Run Plan`
- [x] Wire the final pre-archive review through a fresh controller conversation scoped to the full change
- [x] Write tests for controller-pipeline integration, resume logic, lock handling, scoped staging, and mechanical review prompt

## Phase 5: TUI and Operator Experience
- [ ] Surface controller tool call activity as mechanical status lines in the TUI (switch on tool name, no parsing)
- [ ] Surface execution mode, selected agent, active model, and resume state in the TUI for `Execution Context Visibility`
- [ ] Batch Pi `message_update` events onto the TUI render loop while preserving terminal events for `Pi Event Rendering`
- [ ] Surface Pi restart events in the TUI
- [ ] Extend the exit summary with execution mode for `Exit Summary Includes Mode`
- [ ] Document Pi-first setup, controller configuration, environment variables, and CLI fallback in `README.md`
- [ ] Add end-to-end coverage with a mock Pi RPC process, mock controller API, and a mock CLI agent
