# Pipeline

## Requirements

### Requirement: Branch Creation

The system SHALL create a git branch named `change/<name>` from the current HEAD when starting a run, where `<name>` is derived from the change directory name.

#### Scenario: Clean worktree

- **WHEN** the worktree is clean and no `change/<name>` branch exists
- **THEN** the system creates and checks out `change/<name>`

#### Scenario: Dirty worktree

- **WHEN** the worktree has uncommitted changes
- **THEN** the system aborts with an error before creating any branch

#### Scenario: Branch already exists

- **WHEN** `change/<name>` already exists
- **THEN** the system asks the user to confirm reuse or abort

### Requirement: Artifact Commit

The system SHALL commit all files under the change's `specs/changes/<name>/` directory as the first commit on the new branch with the message `chore: add litespec artifacts for <name>`.

#### Scenario: Initial commit

- **WHEN** the branch is created and artifacts exist
- **THEN** the system stages only the change directory contents and commits

#### Scenario: No artifacts to commit

- **WHEN** the artifacts are already committed (nothing new to stage)
- **THEN** the system skips the commit without error

### Requirement: Prompt Construction

The system SHALL construct prompts that are self-contained and do not reference external skills.

#### Scenario: Apply prompt

- **WHEN** executing a phase
- **THEN** the system sends "Implement phase N of change <name>. The tasks for this phase are: <tasks>"

#### Scenario: Review prompt

- **WHEN** reviewing implementation
- **THEN** the system sends "Review the implementation of change <name>. Report whether it is complete and correct, or list specific issues."

#### Scenario: Fix prompt

- **WHEN** review finds issues
- **THEN** the system sends "The review found these issues: <issues>. Fix them."

#### Scenario: Final review prompt

- **WHEN** running final review
- **THEN** the system sends "Do a final review of the full implementation of change <name>. Confirm everything is complete and ready to archive, or list remaining issues."

### Requirement: Phase Execution

The system SHALL execute phases sequentially by reading `tasks.md`, identifying the current phase (first phase with unchecked tasks), crafting a prompt, and shelling out to the configured agent CLI.

#### Scenario: Normal phase execution

- **WHEN** a phase has unchecked tasks
- **THEN** the system builds a prompt instructing the agent to implement that phase and executes it

#### Scenario: All phases complete

- **WHEN** no unchecked tasks remain in any phase
- **THEN** the system exits the phase loop and proceeds to final review

### Requirement: Phase Commit

The system MUST commit after each successfully reviewed phase with a conventional commit message and a body listing completed tasks.

#### Scenario: Post-review commit

- **WHEN** a phase passes review
- **THEN** the system stages all modified files and commits with subject `feat(<name>): complete phase N - <phase title>` and a body listing each completed task from that phase as a bullet point

### Requirement: Final Review

The system SHALL run a comprehensive review after all phases complete by prompting the agent to review the full implementation.

#### Scenario: Final review pass

- **WHEN** the final review finds no issues
- **THEN** the system commits any final fixes and proceeds to PR creation

#### Scenario: Final review fail

- **WHEN** the final review finds issues
- **THEN** the system enters the retry loop (up to 3 total attempts)

### Requirement: Pull Request Creation

The system SHALL open a pull request by shelling out to `gh pr create` with a title derived from the change name and a body composed from `proposal.md`.

#### Scenario: PR creation

- **WHEN** all phases are complete and reviewed
- **THEN** the system pushes the branch and runs `gh pr create --title "<name>" --body-file proposal.md`

#### Scenario: gh CLI not available

- **WHEN** `gh` is not found on PATH
- **THEN** the system prints the push command and a URL to create the PR manually

### Requirement: Empty Commit Handling

The system MUST skip commits when there are no staged changes, avoiding "nothing to commit" errors.

#### Scenario: No staged changes

- **WHEN** `git diff --cached --quiet` exits 0 (no staged changes)
- **THEN** the system skips the commit and continues without error

#### Scenario: Staged changes exist

- **WHEN** `git diff --cached --quiet` exits 1 (staged changes present)
- **THEN** the system proceeds with the commit
