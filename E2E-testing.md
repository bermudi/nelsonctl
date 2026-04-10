# E2E Testing: nelsonctl Pipeline

## Current Status: Mechanically Functional ✅

The pipeline runs the full state machine without hanging or crashing. All plumbing works:

- Branch creation ✅
- Artifact commit ✅
- Apply (agent creates code) ✅
- Review (agent reviews against spec) ✅
- Fix retry loop (3 attempts) ✅
- Controller drives the loop with tool calls ✅
- Pipeline completes with summary ✅

**The remaining failure mode is agent/review quality, not pipeline mechanics.**

## Reproduction

```bash
cd ~/build/nelsonctl-test
git checkout main
git branch -D change/uppercase-greet 2>/dev/null
git clean -fd
nelsonctl --verbose --no-pr specs/changes/uppercase-greet
```

For debug output:

```bash
NELSONCTL_DEBUG=1 ~/build/nelsonctl/nelsonctl --verbose --no-pr specs/changes/uppercase-greet
```

## Test Project

`~/build/nelsonctl-test/` — a minimal Go project:

- `greet.go` — `greet(name) string` returning `"hello, {name}"`
- `greet_test.go` — test for greet
- `main.go` — empty main
- `specs/changes/uppercase-greet/` — change spec:
  - `proposal.md` — add `UpperGreet` function
  - `specs/greeting/spec.md` — SHALL requirement with scenario
  - `design.md` — wraps `greet()` with `strings.ToUpper()`
  - `tasks.md` — 2 unchecked tasks

## Configuration

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

## Last Run Results (2026-04-10, commit `e6d4453`)

```
State: Init → Branch → CommitArtifacts → PhaseLoop
Phase 1, Attempt 1: Apply (18.8s) → Review (46.4s) → FAIL (CRITICAL: design deviation)
Phase 1, Attempt 2: Fix  (15.8s) → Review (49.1s) → FAIL (tasks not marked done)
Phase 1, Attempt 3: Fix  (8.9s)  → Review (47.6s) → FAIL (tasks still not marked done)
Phase 1: Failed after 3 attempts (5m14s)
State: Done
```

What happened at each step:

| Step | What the agent did | Outcome |
|------|-------------------|---------|
| Apply | Created `upper.go` with `UpperGreet`, created `upper_test.go` with tests, ran `go test` | ✅ Code works, tests pass |
| Review 1 | Found CRITICAL: design says wrap `greet()` but implementation reimplements greeting | ❌ Correct finding |
| Fix 1 | Updated `upper.go` to `strings.ToUpper(greet(name))` | ✅ Fixed the design deviation |
| Review 2 | Found CRITICAL: tasks in `tasks.md` not marked `[x]` | ❌ Correct finding but arguably should be WARNING |
| Fix 2 | Agent claimed to mark tasks as done but didn't actually edit the file | ❌ Agent didn't execute |
| Review 3 | Same CRITICAL: tasks still unchecked | ❌ Same issue |

## Current Blockers

### 1. Agent doesn't reliably execute fix prompts

The fix step tells the agent what to do (e.g., "mark tasks as done"). The agent acknowledges and describes doing it, but sometimes doesn't actually make the change. The controller has no way to verify — it sends the prompt and trusts the result.

**Possible fixes:**
- Controller calls `read_file` on `tasks.md` after each fix to verify the change was applied
- Stronger prompting ("You MUST use your edit tool to change tasks.md. Do not just describe the change.")
- Increase max attempts (3 → 5) to give more chances

### 2. Review severity calibration

The reviewer flags unchecked tasks as CRITICAL. For the `fail_on: critical` threshold, this means a fully implemented and tested change fails because the task list wasn't updated. Whether this is correct behavior depends on how strict we want to be, but for a first pass it's too strict — the implementation is correct, only the process artifact is stale.

**Possible fixes:**
- Tweak the `litespec-review` skill prompt to classify unchecked tasks as WARNING when code exists and passes tests
- Change `fail_on` to `warning` in config (weak — defeats the purpose of strictness)
- Have the controller discount task-tracking findings vs code-correctness findings

## Next Steps (Ordered)

### Step 1: Verify fix execution

Add a verification step to the controller loop. After `submit_prompt` returns from a fix, the controller should call `read_file` on the relevant files to confirm the agent actually made the changes. If not, retry with a more explicit prompt.

**Where**: `internal/controller/prompts.go` — add guidance to the system prompt about verifying fixes. Or `internal/pipeline/pipeline.go` — add a post-fix diff check.

### Step 2: Run again and get a clean pass

After step 1, re-run the `uppercase-greet` test and confirm:
- Phase 1 applies, review passes (or fix + re-review passes)
- Phase gets committed
- Pipeline reaches FinalReview
- Pipeline completes successfully

### Step 3: Test with `--no-pr` off

Run without `--no-pr` to verify the PR creation step works end-to-end:
- Branch pushes to remote
- `gh pr create` succeeds
- PR body includes change summary

### Step 4: Test with a multi-phase change

The current test is single-phase. Create a test change with 2-3 phases to verify the loop commits after each phase and proceeds correctly.

### Step 5: Test edge cases

- Phase where agent produces no changes (should fail review)
- Phase where tests fail (agent should fix)
- Phase where implementation already exists (idempotent behavior)

### Step 6: Remove `NELSONCTL_DEBUG` training wheels

The debug logging is useful but noisy. Replace with structured trace output to a file (the trace system already exists in `internal/trace/`). Make verbose mode useful without debug env var.

## Commits That Got Us Here

| Commit | What it fixed |
|--------|--------------|
| `99dba0b` | Batch fix: model inheritance, controller error handling, DeepSeek content field, text_delta parsing, event draining, activeSessionID, toolUse filter, debug logging |
| `e6d4453` | **The critical fix**: `stopReasonFromRPCEvent` returned first match (always "toolUse") instead of last match ("stop"). Pipeline hung forever on every run before this. |

## Key Technical Reference

### Pi RPC Event Protocol

| Event | Meaning | Notes |
|-------|---------|-------|
| `thinking_start` | Agent starts reasoning | |
| `thinking_delta` | Thinking token chunk | Content in `delta`, not `part.text` |
| `text_delta` | Text output chunk | Content in `delta`, not `part.text` |
| `text_end` | Text output complete | Full text in `content` |
| `message_update` | Full message snapshot | `partial` has complete content |
| `message_end` | Single message complete | Has `usage`, `stopReason` |
| `turn_end` | Conversation turn complete | Agent may continue with tool results |
| `agent_end` | **Agent finished** | Contains ALL messages. Last message's `stopReason` is the one that matters |

### The agent_end Trap

Pi fires `agent_end` once when the agent is truly done. But the event contains the **entire conversation history** — every user/assistant/tool message. Each assistant message has its own `stopReason`. Earlier turns have `"toolUse"` (paused for tool execution), only the last has `"stop"` or `"endTurn"`.

**The bug**: `stopReasonFromRPCEvent` iterated `event.Messages` and returned the first match — always `"toolUse"` from turn 1. The toolUse filter then discarded the event. Pipeline hung forever.

**The fix**: Return the last match, not the first.

### Session Architecture

- **Implementation session**: One per run. `StartImplementationSession` creates it once, reuses on subsequent apply/fix steps. Persisted in `implSessionID`.
- **Review sessions**: Created fresh each time via `new_session` RPC. Forked from the implementation session so the reviewer can see the codebase context.
- **Session ID attribution**: `agent_end` events arrive with empty `sessionId`. The adapter tracks `activeSessionID` as fallback. Works for serial execution only.

### Pipeline State Machine

```
Init → Branch → CommitArtifacts → PhaseLoop → FinalReview → PR → Done
```

PhaseLoop runs for each phase in `tasks.md`:
1. Controller reads artifacts, sends apply prompt via `submit_prompt`
2. Agent implements, returns result
3. Controller calls `run_review` — agent runs `litespec-review`
4. Controller evaluates review output
5. If FAIL: controller sends fix prompt via `submit_prompt`, go to 2
6. If PASS: controller calls `approve`, pipeline commits
7. After all phases: FinalReview runs, then push + PR
