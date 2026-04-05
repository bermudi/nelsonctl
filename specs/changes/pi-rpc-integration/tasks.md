# Tasks

## Phase 1: Runtime Configuration
- [ ] Add config structs, defaults, XDG loading, and validation for `Config File`
- [ ] Implement `nelsonctl init` minimal and advanced flows for `Initialization Wizard`
- [ ] Wire startup checks for `Agent Prerequisite Check`, `Workspace Validation`, and `Credential Handling`
- [ ] Implement execution-mode resolution for `Pi-First Agent Resolution`
- [ ] Write tests for config parsing, environment-driven credentials, and startup validation

## Phase 2: Pi RPC Transport
- [ ] Extend the agent contract to support both CLI and RPC implementations for `Two-Tier Agent Execution`
- [ ] Implement Pi process startup, JSONL framing, request correlation, and typed RPC payloads for `Pi RPC Sessions`
- [ ] Implement apply-session reuse, disposable review sessions, and per-step model switching for `Pi RPC Sessions`
- [ ] Auto-discover workspace skills, disable extensions, and auto-cancel `extension_ui_request` events during Pi runs
- [ ] Implement Pi crash restart behavior for `Pi Crash Recovery`
- [ ] Write tests for RPC framing, session lifecycle, and crash recovery

## Phase 3: Review Intelligence
- [ ] Implement fuzzy severity-section and scorecard parsing for `Three-Tier Review Detection`
- [ ] Add heuristic fallback and issue-vs-scorecard conflict handling for `Three-Tier Review Detection`
- [ ] Implement `review.fail_on` parsing and enforcement for `Review Failure Threshold`
- [ ] Implement raw HTTP controller summarizer clients for OpenAI, Anthropic, and OpenRouter for `Controller Summarizer`
- [ ] Wire compact parsed issues into fix steering for `Parsed Retry Loop`
- [ ] Write tests for structured parsing, ambiguity fallback, threshold handling, and provider selection

## Phase 4: Pipeline Resume and Safety
- [ ] Update the orchestrator to choose Nelson mode vs Ralph mode at runtime for `Pi-Aware Phase Execution`
- [ ] Implement branch reuse, first-unchecked-phase resume, and recovery commits for `Resumable Change Branch` and `Recovery Commits and Scoped Staging`
- [ ] Add `.nelsonctl.lock` creation, stale PID cleanup, and lock release for `Run Locking`
- [ ] Stage only changed files via `git diff --name-only` while preserving artifact-only staging for the first artifact commit
- [ ] Implement the dry-run planner for `Dry-Run Plan`
- [ ] Update final review handling to use the same parsing and threshold policy as phase reviews for `Final Review Gate`
- [ ] Write tests for resume logic, lock handling, scoped staging, and smart-vs-dumb path selection

## Phase 5: TUI and Operator Experience
- [ ] Surface execution mode, selected agent, active model, and resume state in the TUI for `Execution Context Visibility`
- [ ] Batch Pi `message_update` events onto the TUI render loop while preserving terminal events for `Pi Event Rendering`
- [ ] Surface Pi restart events and keep the Nelson taunt behavior aligned with the modified retry loop
- [ ] Extend the exit summary with execution mode for `Exit Summary Includes Mode`
- [ ] Document Pi-first setup, config, environment variables, and explicit CLI fallback in `README.md`
- [ ] Add end-to-end coverage with a mock Pi RPC process and a mock CLI agent
