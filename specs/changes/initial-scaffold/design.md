# nelsonctl — Design Document

A Go TUI that automates the litespec apply → review → fix loop. Named after Nelson Muntz, continuing the Simpsons tradition from Ralph Loop. "HA-ha!" at your failed reviews.

## Stack

- **Language:** Go
- **Module:** `github.com/bermudi/nelsonctl`
- **TUI:** [Bubble Tea](https://github.com/charmbracelet/bubbletea) + [Lip Gloss](https://github.com/charmbracelet/lipgloss) + [Bubbles](https://github.com/charmbracelet/bubbles)
- **Git:** shells out to `git` CLI
- **PR:** shells out to `gh` CLI
- **Agents:** shells out to agent CLIs (`opencode`, `claude`, `codex`, `amp`)

## Directory Structure

```
nelsonctl/
├── cmd/nelsonctl/
│   └── main.go              # CLI entry point, flag parsing
├── internal/
│   ├── agent/
│   │   ├── adapter.go        # Agent CLI abstraction interface
│   │   ├── opencode.go       # opencode invocation
│   │   ├── claude.go         # claude invocation
│   │   ├── codex.go          # codex invocation
│   │   └── amp.go            # amp invocation
│   ├── pipeline/
│   │   ├── pipeline.go       # Main pipeline orchestrator
│   │   ├── phase.go          # Phase parsing from tasks.md
│   │   └── prompt.go         # Prompt construction for apply/review/fix
│   ├── git/
│   │   └── git.go            # Branch, commit, push, PR operations
│   ├── review/
│   │   └── review.go         # Review result parsing, retry logic
│   └── tui/
│       ├── model.go          # Bubble Tea model
│       ├── view.go           # Layout rendering
│       ├── update.go         # Message handling
│       └── styles.go         # Lip Gloss styles
├── go.mod
└── go.sum
```

## Architecture

```
┌─────────────────────────────────────────┐
│              TUI (Bubble Tea)           │
│  ┌──────────┐  ┌──────────────────────┐ │
│  │ Progress  │  │   Agent Output       │ │
│  │ Panel     │  │   (streaming)        │ │
│  └──────────┘  └──────────────────────┘ │
└────────────────────┬────────────────────┘
                     │ messages
              ┌──────▼──────┐
              │  Pipeline   │
              │ Orchestrator│
              └──┬───┬───┬──┘
                 │   │   │
         ┌───────┘   │   └───────┐
         ▼           ▼           ▼
    ┌─────────┐ ┌─────────┐ ┌─────────┐
    │  Agent  │ │   Git   │ │ Review  │
    │ Adapter │ │  Ops    │ │ Parser  │
    └─────────┘ └─────────┘ └─────────┘
```

### Pipeline Orchestrator

The core state machine. Receives the change path, reads `tasks.md`, and drives the loop:

```
Init → Branch → CommitArtifacts → PhaseLoop → FinalReview → PushAndPR → Done
                                      │
                                 ┌────▼────┐
                                 │ Apply   │
                                 │ Review  │◄─┐
                                 │ Fix?    │──┘ (max 3 attempts)
                                 │ Commit  │
                                 └─────────┘
```

The pipeline emits messages to the TUI model (phase started, attempt N, output chunk, review result, phase done). The TUI is a passive consumer — it never drives the pipeline.

### Agent Adapter

Interface:

```go
type Agent interface {
    Name() string
    Available() error
    Run(ctx context.Context, prompt string, workDir string) (*Result, error)
}

type Result struct {
    Stdout   string
    Stderr   string
    ExitCode int
    Duration time.Duration
}
```

Each agent implementation builds the correct CLI invocation. Output is streamed via a callback for the TUI to consume in real time.

### Prompt Construction

Three prompt types, all referencing litespec skills the agent already has:

1. **Apply prompt:** "Use your litespec-apply skill to implement phase {N} of change {name}. The tasks for this phase are: {tasks}"
2. **Review prompt:** "Use your litespec-review skill to review the implementation of change {name}."
3. **Fix prompt:** "The review found these issues: {review_output}. Fix them."

The prompts are simple because the skills carry all the context. nelsonctl is just the scheduler.

### Git Operations

All git operations shell out to `git` CLI:

- `git checkout -b change/<name>` — branch creation
- `git add specs/changes/<name>/` — stage artifacts
- `git commit -m "subject" -m "body"` — commit with subject + task list body
- `git push -u origin change/<name>` — push
- `gh pr create --title "..." --body-file proposal.md` — PR

### Review Result Detection

The review step needs to determine pass/fail. Strategy:

1. Agent runs review skill, which outputs structured assessment
2. nelsonctl scans the output for failure indicators (e.g., "issues found", "FAIL", non-zero exit code)
3. If ambiguous, treat as pass (optimistic — the final review catches anything missed)

## Key Design Decisions

- **No litespec dependency at runtime** — nelsonctl reads `tasks.md` directly (simple markdown parsing) and constructs prompts. It never imports litespec or calls its CLI.
- **Agent-agnostic** — the adapter interface means adding a new agent is one file implementing `Run()`.
- **TUI is passive** — the pipeline runs in a goroutine and sends messages. The TUI renders. Clean separation.
- **Optimistic commits** — commit after each phase even if the review had issues on the final retry. Better to have progress on the branch than lose work.
- **`gh` is optional** — if not available, print manual instructions. Don't block the pipeline on tooling.

## CLI Interface

```
nelsonctl <change-path> [flags]

Arguments:
  change-path    Path to the litespec change directory (e.g., specs/changes/add-dark-mode)

Flags:
  --agent string    Agent CLI to use (default: opencode) [opencode|claude|codex|amp]
  --timeout dur     Timeout per agent invocation (default: 10m)
  --dry-run         Show what would be done without executing
  --no-pr           Skip PR creation
  --verbose         Show full agent output (default: streaming summary)
```
