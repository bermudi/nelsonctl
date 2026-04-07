# Resumable Pipeline

## ADDED Requirements

### Requirement: Resumable Change Branch
The system SHALL create `change/<name>` the first time a change runs and SHALL reuse that same branch on later runs for the same change. It MUST refuse to start from an unrelated dirty branch, but it SHALL resume on the existing change branch by preserving or committing recoverable work before issuing new agent prompts.

#### Scenario: First run
- **WHEN** the worktree is clean and `change/<name>` does not exist
- **THEN** nelsonctl creates and checks out `change/<name>`

#### Scenario: Resume on existing change branch
- **WHEN** `change/<name>` already exists and `tasks.md` contains checked tasks
- **THEN** nelsonctl checks out the existing branch and resumes the pipeline without prompting for branch reuse

#### Scenario: Dirty unrelated branch
- **WHEN** the current branch is not `change/<name>` and the worktree contains uncommitted changes
- **THEN** nelsonctl aborts before changing branches

### Requirement: Pi-Aware Phase Execution
The system SHALL execute phases sequentially starting from the first unchecked task in `tasks.md`. The controller AI drives all phases via `submit_prompt` and `run_review`, regardless of whether the underlying agent is RPC-capable or CLI-only. When the agent supports RPC, apply and fix MUST run through the long-lived implementation session and review MUST run through disposable review sessions; when the agent is CLI-only, the controller's `submit_prompt` and `run_review` calls route to CLI shell-out invocations.

#### Scenario: Resume at later phase
- **WHEN** phases 1 and 2 are already fully checked in `tasks.md`
- **THEN** nelsonctl begins execution at phase 3

#### Scenario: Pi smart path
- **WHEN** the effective agent is `pi`
- **THEN** the controller drives apply, review, and fix through the Pi RPC session lifecycle

#### Scenario: CLI path via controller
- **WHEN** the effective agent is `claude`
- **THEN** the controller drives apply, review, and fix by routing `submit_prompt` and `run_review` to CLI shell-out invocations

### Requirement: Recovery Commits and Scoped Staging
The system MUST commit after each phase using only files returned by `git diff --name-only`. If a resumed run starts on the change branch with uncommitted tracked changes, the system SHALL create a recovery commit before it sends any new apply prompt.

#### Scenario: Scoped phase commit
- **WHEN** a phase review passes
- **THEN** nelsonctl stages only the changed files reported by git diff and commits them with the phase message

#### Scenario: Recovery commit on resume
- **WHEN** nelsonctl resumes a change branch that has uncommitted tracked modifications from a previous interrupted run
- **THEN** it creates a recovery commit before asking the agent to continue

### Requirement: Final Review Gate
The system SHALL run the final pre-archive review through a fresh controller conversation scoped to the full change. The controller reasons about the review output and applies the configured `review.fail_on` threshold through comprehension, following the same tool-calling pattern as phase reviews.

#### Scenario: Final review in Pi mode
- **WHEN** all phases are complete and the effective agent is `pi`
- **THEN** the controller runs the pre-archive review via `run_review`, which creates a fresh Pi review session using the configured review model

#### Scenario: Final review failure
- **WHEN** the final review produces issues that the controller determines are at or above the configured failure threshold
- **THEN** the controller crafts fix prompts and the pipeline enters the same fix-review loop used for phase reviews, subject to the 3-attempt retry budget

### Requirement: Workspace Validation
The system SHALL validate that the working directory is a litespec project before starting a run. It MUST confirm that `specs/` exists and that `.agents/skills/litespec-apply/` and `.agents/skills/litespec-review/` are available before acquiring the run lock or invoking an agent.

#### Scenario: Missing specs directory
- **WHEN** the current working directory does not contain `specs/`
- **THEN** nelsonctl exits with a validation error before the pipeline starts

#### Scenario: Missing review skill
- **WHEN** `.agents/skills/litespec-review/` is not present in the workspace
- **THEN** nelsonctl exits with a startup error explaining the missing prerequisite

### Requirement: Run Locking
The system SHALL create a lock file at `specs/changes/<name>/.nelsonctl.lock` containing the current PID and timestamp, and it MUST detect stale locks before starting work.

#### Scenario: Active lock
- **WHEN** the lock file points at a live process
- **THEN** nelsonctl aborts the run with a message that the change is already in progress

#### Scenario: Stale lock
- **WHEN** the lock file points at a dead PID
- **THEN** nelsonctl removes the stale lock and continues startup

### Requirement: Dry-Run Plan
The system SHALL print a non-executing plan when `--dry-run` is enabled that lists the phases to run, the tasks in each remaining phase, the detected execution mode, the selected agent, the per-step models, and the review failure threshold.

#### Scenario: Dry run on partially completed change
- **WHEN** `--dry-run` is enabled and earlier phases are already checked off
- **THEN** nelsonctl prints only the remaining phases along with the resolved run configuration and exits without creating a lock, branch, or agent process
