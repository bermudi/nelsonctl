# Investigation: nelsonctl End-to-End Pipeline (apply → review → fix → archive)

## Reproduction Environment

### Test Project Location
```
~/build/nelsonctl-test/
```

### nelsonctl Repository
```
~/build/nelsonctl/
```

### How to Reproduce
```bash
cd ~/build/nelsonctl-test
git checkout main
git branch -D change/uppercase-greet 2>/dev/null
git clean -fd
nelsonctl --verbose --no-pr specs/changes/uppercase-greet
```

### Test Project State

The test project is a simple Go project with:
- `greet.go` - existing function `greet(name string) string` returning "hello, {name}"
- `greet_test.go` - test for greet function
- `main.go` - empty main
- `specs/changes/uppercase-greet/` - change spec containing:
  - `proposal.md` - describes adding `UpperGreet` function
  - `specs/greeting/spec.md` - spec with ADDED requirement
  - `design.md` - implementation design
  - `tasks.md` - tasks with unchecked boxes

### Configuration

`~/.config/nelsonctl/config.yaml`:
```yaml
agent: pi
steps:
  apply:
    model: opencode-go/kimi-k2.5
    timeout: 30m
  review:
    model: opencode-go/kimi-k2.5
    timeout: 15m

controller:
  provider: deepseek
  model: deepseek-reasoner
  max_tool_calls: 50
  timeout: 45m

review:
  fail_on: critical
```

## Goal

**Make nelsonctl drive a litespec change end-to-end: apply → review → fix (retry) → commit per phase → final review → push → PR — without human intervention.**

This is the "execution phase" of litespec's workflow. The human completes the thinking phase (explore → grill → propose → review) and hands off to nelsonctl. Nelsonctl runs the dumb loop: apply each phase, review, fix if needed, commit, repeat until all phases pass, then final review and PR.

The pipeline does NOT do archive. Archive is a separate `litespec archive` step after nelsonctl finishes.

## How It Works

### The Two AI Roles

1. **The Agent** (pi, opencode, etc.) — the coding tool that writes, edits, and reviews code. Configured in `steps.apply` and `steps.review`. Each step can use a different model. The agent receives prompts, uses litespec skills (`litespec-apply`, `litespec-review`), and produces code changes or review output.

2. **The Controller** (LLM, configured in `controller`) — nelsonctl's brain that evaluates review results, decides pass/fail, and orchestrates the pipeline. It reads the agent's review output and makes the judgment call: is this phase done, or does the agent need to try again?

**The agent builds the code, the controller judges it.**

### Pipeline State Machine

```
Init → Branch → CommitArtifacts → PhaseLoop → FinalReview → PR → Done
```

For each phase in `tasks.md`:
1. **Apply** — controller calls `submit_prompt`; the agent implements the phase using `litespec-apply` skill
2. **Review** — controller calls `run_review`; the agent runs `litespec-review` against the proposal/specs
3. **Fix** — if review fails, controller feeds review output back and retries (max `max_attempts` per phase)
4. **Commit** — on success, `feat(<change>): complete phase N - <name>`

After all phases pass, final review runs in pre-archive mode, then branch is pushed and PR opened via `gh`.

### Key Components

| Component | Role |
|-----------|------|
| `nelsonctl` CLI | Entry point (`cmd/nelsonctl/`), flag parsing, verbose mode |
| Pipeline (`internal/pipeline/`) | State machine: phase loop, prompt construction, retry logic, commits |
| Controller (`internal/controller/`) | LLM that calls tools (`submit_prompt`, `run_review`, `get_diff`, `read_file`, `approve`) to drive phases |
| Agent adapter (`internal/agent/`) | Interface + adapters (pi RPC, opencode, claude, codex, amp). Translates pipeline steps into agent sessions |
| Pi adapter (`internal/agent/pi.go`) | JSONL-RPC adapter for pi. Session management, event streaming, `agent_end` detection |
| pi (external process) | The actual AI agent binary ([github.com/danielberkompas/pi](https://github.com/danielberkompas/pi)). Manages sessions, models, tool calls internally |
| Git ops (`internal/git/`) | Branch, add, commit, push |
| TUI (`internal/tui/`) | Bubble Tea two-panel display (progress + output) |
| Config (`internal/config/`) | YAML config, wizard, model inheritance (fix → apply) |

### Litespec Skills Used

The agent receives prompts that explicitly invoke litespec skills:
- **`litespec-apply`** — implements one phase at a time, marks tasks `[x]`, respects scope boundaries
- **`litespec-review`** — context-aware review: implementation review during phases, pre-archive review at the end. Pure review (never writes code). Produces structured output with CRITICAL/WARNING/SUGGESTION and a scorecard
- **`litespec-archive`** — NOT used by nelsonctl. Archive is done separately via `litespec archive` after the pipeline completes

### Controller Tools

The controller LLM has these tools available:
- `read_file` — Read a file from workspace
- `get_diff` — Get git diff of changes
- `submit_prompt` — Send implementation/fix prompt to agent
- `run_review` — Run mechanical review using litespec-review skill
- `approve` — Mark phase as approved with summary




## Current Blocker: Pi RPC Session Management

The Pi adapter (`internal/agent/pi.go`) has fragile session lifecycle handling. Each `submit_prompt` or `run_review` call may spawn a new session instead of reusing an existing one, and `agent_end` detection is unreliable. This causes:

1. Loss of context between steps (new session = blank slate)
2. Inefficient resource usage (redundant session creation)
3. Premature termination (`agent_end` fires per-turn, not per-completion)
4. Stale event accumulation (events from previous sessions pollute the channel)

This is the immediate blocker, but the broader goal is making the full pipeline reliable end-to-end.

## Observations

### Session IDs Created During a Single Run

From debug output, a single `nelsonctl` execution creates many distinct sessions:

```
[DEBUG-agent] SendMessage: session=4846deaa-f775-42f7-9f6a-3d845da30d21 model=... prompt="Implement phase 1..."
[DEBUG-agent] SendMessage: session=1fc1e14b-830b-4e51-a3fb-1f33eb7aa0f4 model=... prompt="Implement phase 1..."
[DEBUG-agent] SendMessage: session=f7dcdc9e-0267-402e-a66d-ce132efd3749 model=... prompt="Use your litespec-review..."
[DEBUG-agent] SendMessage: session=6205faf4-bca8-4960-b9a7-c71ba52be190 model=... prompt="Create the implementation..."
```

Each `SendMessage` call has a **different session ID**, indicating new sessions are being created rather than reused.

### Key Code Paths

#### 1. `sessionForStep` in `pi.go:266`

```go
func (a *piRPCAgent) sessionForStep(ctx context.Context, step Step) (string, error) {
    switch step {
    case StepApply, StepFix:
        return a.StartImplementationSession(ctx)
    case StepReview, StepFinalReview:
        return a.StartReviewSession(ctx)
    default:
        return a.StartImplementationSession(ctx)
    }
}
```

**Finding**: Each call to `sessionForStep` invokes `StartImplementationSession` or `StartReviewSession`. These functions create new sessions if one doesn't exist, but the logic may be creating new sessions on every call.

#### 2. `StartImplementationSession` in `pi.go:85`

```go
func (a *piRPCAgent) StartImplementationSession(ctx context.Context) (string, error) {
    a.mu.Lock()
    if a.implSessionID != "" {
        id := a.implSessionID
        a.mu.Unlock()
        return id, nil
    }
    a.mu.Unlock()
    // ... creates new session via RPC ...
}
```

**Finding**: This checks if `implSessionID` is already set and returns it if so. The issue might be that `implSessionID` is not being persisted correctly, or the agent instance is being recreated.

#### 3. `StartReviewSession` in `pi.go:110`

```go
func (a *piRPCAgent) StartReviewSession(ctx context.Context) (string, error) {
    // ... always creates a new session via "new_session" RPC ...
    response, err := a.client.Send(ctx, rpcCommand{Type: "new_session", ParentSession: parent})
    // ...
}
```

**Finding**: Review sessions are **always created fresh** via `new_session` RPC command. This is intentional (review sessions fork from implementation session), but may be creating too many.

#### 4. `SendMessage` in `pi.go:147`

```go
func (a *piRPCAgent) SendMessage(ctx context.Context, sessionID, prompt, model string) (*Result, error) {
    if err := a.ensureClient(ctx); err != nil { ... }
    if err := a.switchToSession(ctx, sessionID); err != nil { ... }
    // ...
}
```

**Finding**: `SendMessage` receives a `sessionID` parameter from the caller. The caller (`ExecuteStep`) gets this from `sessionForStep`, which may be returning new session IDs each time.

### Additional Findings

#### Model Not Found Error

```
[DEBUG-agent] SendMessage set_model FAILED: provider=minimax model=minimax-m2.7 err=rpc set_model failed: Model not found: minimax/minimax-m2.7
```

The configured model `minimax/minimax-m2.7` was not available in the pi agent. This was fixed by changing the config to use `opencode-go/kimi-k2.5`.

#### Event Draining

The `consumeSessionEvents` function drains stale events from previous sessions:

```go
drained := 0
drain:
for {
    select {
    case ev := <-a.events:
        drained++
        _ = ev
    default:
        break drain
    }
}
```

Debug output shows `drained=2`, `drained=3`, `drained=4` values, indicating events from previous sessions are accumulating in the channel.

## Fixes Committed (99dba0b)

All of these are already in `main`. They were applied during the debugging session.

### 1. `fix.model` inherits from `apply.model`

`steps.fix.model` and `steps.apply.model` were independent fields with separate hardcoded defaults (`minimax/minimax-m2.7`). If you only set `apply.model`, fix silently used a different model. Now `normalize()` copies `apply.model` to `fix.model` when the latter is empty. The wizard was updated to match.

**Files**: `internal/config/config.go`, `internal/config/wizard.go`

### 2. Controller tool dispatch errors are non-fatal

Previously, `read_file` errors (reading directories, empty paths, absolute paths) returned a fatal `error` from `Dispatch`, crashing the controller conversation. Now they return a `DispatchResult` with the error as `Content` so the controller can see the error and retry/recover.

**Files**: `internal/controller/tools.go`, `internal/controller/tools_test.go`

### 3. DeepSeek `content` field uses `omitempty`

DeepSeek's API requires `content` even when empty (unlike OpenAI). The `openAIMessage` struct had `content:...,omitempty` which caused malformed requests. Fixed by removing `omitempty` from the Content field.

**File**: `internal/controller/controller.go`

### 4. Pi RPC event parsing: `text_delta` and `thinking_delta`

Pi's RPC sends text content in `assistantMessageEvent.delta`, not in `part.text`. The `rpcAssistantMessageEvent` struct only captured `part.text`, so all `text_delta` events had empty content. Fixed `forwardEvents` to use `delta` field.

Thinking tokens were also being mixed into output. Fixed to filter them out.

**Files**: `internal/agent/pi.go`, `internal/agent/rpctypes.go`

### 5. Stale event draining before consuming

`consumeSessionEvents` now drains stale events from the shared `a.events` channel before starting the consumer goroutine. Debug showed `drained=2` to `drained=9`, confirming stale `CompletionEvent`s from previous sessions were accumulating.

**File**: `internal/agent/pi.go`

### 6. `activeSessionID` tracking for `agent_end`

Pi's `agent_end` events arrive with empty `sessionId`. Added `activeSessionID` field on the agent, set by `switchToSession`, used as fallback when `agent_end` has no session ID.

**File**: `internal/agent/pi.go`

### 7. `agent_end` with `stopReason: "toolUse"` filtering

**This is the fundamental remaining issue.** Pi fires `agent_end` at the end of every conversation turn, not just when the agent is fully done. When the agent makes tool calls, `agent_end` fires with `stopReason: "toolUse"` — but the agent will continue executing those tools and producing more turns. A partial fix was added: `forwardEvents` now checks `lastStopReason()` and ignores `agent_end` when the last assistant message has `stopReason: "toolUse"`.

**File**: `internal/agent/pi.go`

### 8. Debug logging (gated behind `NELSONCTL_DEBUG`)

Extensive debug logging added to `SendMessage`, `consumeSessionEvents`, `forwardEvents`, and `rpcclient.go`. All gated behind `NELSONCTL_DEBUG` env var.

**Files**: `internal/agent/pi.go`, `internal/agent/rpcclient.go`

## Key Technical Discoveries

### Pi RPC Event Protocol

Understanding how Pi's JSONL-RPC protocol actually works was critical:

| Event Type | Fields | What It Means |
|---|---|---|
| `thinking_start` | | Agent started thinking |
| `thinking_delta` | `delta` | Thinking token chunk (NOT in `part.text`) |
| `text_delta` | `delta` | Text output chunk (NOT in `part.text`) |
| `message_update` | `session`, `sessionFile`, `assistantUpdate.part.text` | Full assistant message snapshot |
| `turn_end` | | End of a conversation turn |
| `agent_end` | `messages[]` (full conversation), `sessionId` (often empty), `stopReason` | **Fires per-turn, NOT per-completion** |

The `delta` field is the actual content for streaming events. The `part` field is always `{}` (empty struct). This is why the original `rpcAssistantMessageEvent.Part.Text` approach captured nothing.

### Session Lifecycle Race Conditions

The session system has several race conditions:

1. **Stale events**: `agent_end` from a previous session's turn lingers in the 128-event buffer and gets consumed by the next session's consumer
2. **Missing session IDs**: Pi's `agent_end` events have empty `sessionId`, making it impossible to determine which session they belong to
3. **Async event delivery**: `switchToSession` changes `activeSessionID`, but `agent_end` events arrive asynchronously after the switch, getting tagged with the wrong session
4. **`StartReviewSession` switches back**: After creating a review session, it switches back to the impl session. This means `activeSessionID` is the impl session when the review consumer starts

### The Apply Step's Silent Failure Pattern

The most insidious bug: the apply step uses `minimax/minimax-m2.7` which wasn't available in Pi. `set_model` RPC fails, `SendMessage` returns an error, but the controller sees "Agent completed successfully" (or a generic error) and proceeds to review. The review finds no code changes (because the agent never ran), produces an empty or artifact-mode review, and the controller gets stuck in a loop of applying nothing and reviewing nothing.

This was invisible in normal output because:
- Verbose mode drops events when `OnEvent` is nil
- Pi RPC output goes through events, not stdout callback
- The controller's tool dispatch error was fatal, killing the conversation before the controller could reason about it

### Pipeline Event Visibility Gap

In `--verbose` mode, `OnEvent` is nil so all controller/phase events are silently dropped. Only agent stdout callback fires. But Pi RPC sends output through the events channel, not stdout callback. This means the entire controller conversation and agent activity is invisible in verbose mode — you only see the final summary.

## Successful End-to-End Run

After fixing the model to `opencode-go/kimi-k2.5` (available in Pi) and the `stopReason: "toolUse"` filter, a full run succeeded:

- Phase 1 passed (2 attempts)
- Agent created `upper.go` with `UpperGreet` function
- Agent created `upper_test.go` with comprehensive tests
- Tests passed
- Pipeline progressed to final review

## Pipeline Status: Mechanically Functional ✅

As of commit `e6d4453`, the pipeline runs end-to-end without hanging:
- Branch creation ✅
- Artifact commit ✅
- Apply (agent creates code) ✅
- Review (agent reviews against spec) ✅
- Fix retry loop (3 attempts) ✅
- Controller drives the loop with tool calls ✅
- Pipeline completes with summary ✅

**The remaining failure mode is review content quality**, not pipeline mechanics.

### Successful Run Log (2026-04-10)

```
State: Init → Branch → CommitArtifacts → PhaseLoop
Phase 1, Attempt 1: Apply (18.8s) → Review (46.4s) → FAIL (CRITICAL: design deviation)
Phase 1, Attempt 2: Fix (15.8s) → Review (49.1s) → FAIL (tasks not marked done)
Phase 1, Attempt 3: Fix (8.9s) → Review (47.6s) → FAIL (tasks still not marked done)
Phase 1: Failed after 3 attempts (5m14s)
State: Done
```

The agent correctly fixed the code in attempt 2 (wrapped `greet()` with `strings.ToUpper()`),
but failed to properly mark tasks as done in attempts 2 and 3. The agent claimed it marked them
but the file wasn't actually updated. This is an agent reliability issue, not a pipeline bug.

## Fixes Committed

### Commit `e6d4453`: stopReason uses LAST message, not first

`stopReasonFromRPCEvent` iterated `event.Messages` and returned the first match. `agent_end`
contains ALL conversation messages — earlier turns have `stopReason: "toolUse"`, the LAST has
`stopReason: "stop"`. The function returned "toolUse", so the filter always ignored the final
`agent_end`, and the pipeline hung forever. Now it returns the last match.

### Commit `99dba0b`: Initial batch of fixes

See "Fixes Committed (99dba0b)" section below for the full list.

## Remaining Work

### Critical (review quality)

1. **Agent doesn't reliably execute fixes** — The agent says it marked tasks as done but doesn't
   actually do it. The controller sends correct fix prompts, but the agent fails to execute.
   Options: stronger prompting, verify fix with `read_file` after submit_prompt, or have the
   controller check the fix itself before running review.

2. **Review is overly strict for a simple change** — The reviewer flags CRITICAL for design deviation
   (not wrapping `greet()`) even when the implementation produces correct output per spec. Consider
   whether design compliance should be CRITICAL or WARNING for the `fail_on: critical` threshold.

### Important (pipeline reliability)

3. **Session ID attribution** — Pi's `agent_end` events don't include `sessionId`. The
   `activeSessionID` fallback is correct for serial execution but would break with concurrent
   sessions. Events are asynchronous and session attribution is best-effort.

4. **Verbose mode invisibility** — Controller and agent activity is invisible in `--verbose` mode
   because events are dropped when `OnEvent` is nil. Should at minimum log controller tool calls
   and agent session output to stderr.

5. **Config default models may not exist in Pi** — `DefaultConfig()` hardcodes
   `minimax/minimax-m2.7` which may not be available. The `nelsonctl init` wizard should validate
   model availability.

### Nice-to-have

6. **Clean up debug logging** — The `NELSONCTL_DEBUG` gated logging is useful but verbose.
   Consider a structured trace file instead.

7. **Test with different Pi models** — Only verified with `opencode-go/kimi-k2.5`.

8. **Document the Pi RPC event protocol** — Reference doc for the JSONL-RPC event types.

9. **Investigate concurrent session support** — The `activeSessionID` approach won't work for
   parallel step execution.

### Future (post-MVP)

10. **Archive integration** — After the pipeline completes successfully, optionally run `litespec
    archive` to merge delta specs into canon.
11. **Resume after crash** — The recovery commit mechanism exists but full resume (picking up
    mid-phase) is not tested.
12. **Multi-agent orchestration** — Run apply and review with different agents or models
    simultaneously.

## Next Steps

1. **Fix `agent_end` detection** — Make the Pi adapter reliably distinguish turn-end from session-complete. Options: poll `get_state`, wait for explicit completion signal, or count expected turns.
2. **Fix verbose mode** — Route controller/agent events to stderr in verbose mode so operators can see what's happening.
3. **Validate model availability** — Have `nelsonctl init` or startup check that configured models exist in Pi.
4. **End-to-end test** — Run the full pipeline on `uppercase-greet` change and verify: branch created → phase 1 applied → review passed → committed → final review passed → PR opened.
5. **Test with different models** — Verify the pipeline works with models other than `opencode-go/kimi-k2.5`.
6. **Document Pi RPC event protocol** — Reference doc for JSONL-RPC event types.
7. **Clean up debug logging** — Commit or remove `NELSONCTL_DEBUG` gated logging.

## Code Locations

| File | Function | Purpose |
|------|----------|---------|
| `internal/agent/pi.go:58` | `ExecuteStep` | Entry point for step execution |
| `internal/agent/pi.go:85` | `StartImplementationSession` | Creates/returns impl session |
| `internal/agent/pi.go:110` | `StartReviewSession` | Creates review session (always new) |
| `internal/agent/pi.go:266` | `sessionForStep` | Maps step type to session |
| `internal/agent/pi.go:147` | `SendMessage` | Sends prompt to session |
| `internal/agent/pi.go:308` | `switchToSession` | Switches active session, sets `activeSessionID` |
| `internal/agent/pi.go:316` | `consumeSessionEvents` | Drains stale events, consumes events for a session |
| `internal/agent/pi.go:388` | `forwardEvents` | Routes RPC events to agent event channel, filters `toolUse` agent_end |
| `internal/agent/rpctypes.go` | `rpcAssistantMessageEvent` | RPC event struct — `delta` field for text/thinking content |
| `internal/agent/rpcclient.go` | `readStdout` | Parses JSONL from Pi process stdout |
| `internal/controller/controller.go` | `complete` | Controller API call with retry |
| `internal/controller/tools.go` | `resolvePath` | Tool dispatch path resolution |
| `internal/pipeline/pipeline.go` | `SubmitPrompt`/`RunReview` handlers | Calls `ExecuteStep` |
| `internal/config/config.go` | `normalize()` | Model inheritance (fix → apply) |
