# Tasks

## Phase 1: Foundation
- [ ] Initialize Go module (`github.com/bermudi/nelsonctl`)
- [ ] Set up project structure (`cmd/nelsonctl/`, `internal/`)
- [ ] Implement `tasks.md` parser — extract phases and tasks from markdown checkboxes
- [ ] Implement git operations — branch, add, commit, push
- [ ] Write tests for parser and git helpers

## Phase 2: Agent Adapter
- [ ] Define `Agent` interface (Name, Available, Run)
- [ ] Implement `opencode` adapter with correct CLI flags
- [ ] Implement `claude` adapter
- [ ] Implement `codex` adapter
- [ ] Implement `amp` adapter
- [ ] Implement agent availability check (verify binary on PATH)
- [ ] Implement output streaming via stdout pipe callback
- [ ] Implement timeout + SIGTERM/SIGKILL termination
- [ ] Write tests for command building and adapter selection

## Phase 3: Pipeline Orchestrator
- [ ] Implement pipeline state machine (Init → Branch → CommitArtifacts → PhaseLoop → FinalReview → PR → Done)
- [ ] Implement prompt construction (apply, review, fix) referencing litespec skills
- [ ] Implement review result detection (pass/fail from agent output)
- [ ] Implement retry loop (3 total attempts per phase)
- [ ] Implement phase commit with conventional message format
- [ ] Implement final review (pre-archive mode prompt)
- [ ] Implement PR creation via `gh` with fallback to manual instructions
- [ ] Write tests for pipeline state transitions and retry logic

## Phase 4: TUI
- [ ] Set up Bubble Tea model with two-panel layout (progress + output)
- [ ] Implement phase progress panel (pending/running/passed/failed indicators)
- [ ] Implement agent output streaming panel with scrollback
- [ ] Implement retry counter display
- [ ] Implement keybindings (q/ctrl+c abort, p pause, j/k scroll)
- [ ] Implement exit summary (phases completed, failed, duration, branch)
- [ ] Wire pipeline messages to TUI updates via channels
- [ ] Write tests for TUI model state transitions

## Phase 5: Integration & Polish
- [ ] Wire CLI flag parsing (--agent, --timeout, --dry-run, --no-pr, --verbose)
- [ ] End-to-end test with a mock agent (script that simulates apply/review/fix)
- [ ] Add `--dry-run` mode that prints pipeline plan without executing
- [ ] Write README with usage examples
