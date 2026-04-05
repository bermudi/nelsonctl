# nelsonctl

## Motivation

After a user completes the thinking phase of spec-driven development (explore → grill → propose → review), there's a manual, tedious execution loop: create a branch, commit artifacts, implement each phase, review, fix, commit, repeat, open a PR. This is mechanical work that an orchestrator can automate.

Ralph Loop proved that stubborn autonomous iteration works. T3 Code proved that a good UI over agent CLIs adds real value. nelsonctl combines both ideas into a TUI that drives the apply → review → fix loop automatically, phase by phase.

## Scope

A Go TUI application that takes a litespec change directory and executes the full implementation pipeline autonomously:

1. Creates a git branch
2. Commits the planning artifacts
3. For each phase in `tasks.md`: shells out to an AI agent CLI with a crafted prompt, reviews via the agent's litespec-review skill, ralph-loops on failures (max 3 attempts), and commits on success
4. Runs a final comprehensive review
5. Opens a pull request

The TUI provides real-time visibility into the loop: which phase, which attempt, agent output streaming, review results.

## Non-Goals

- Not a chat interface — no interactive prompting. It's a pipeline runner.
- Not a model provider — it shells out to existing agent CLIs (opencode, claude, codex, amp).
- Not a replacement for litespec — it reads litespec artifacts but never writes them directly. The agent does that through skills.
- Not multi-repo or multi-change — one change, one run, one branch.
