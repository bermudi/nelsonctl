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

Reset the test repo to a clean state:

```bash
cd ~/build/nelsonctl
bash e2e.sh
```

This resets `~/build/nelsonctl-test/` to commit `fb27c76b592` (litespec artifacts only), deletes any `change/guestbook-spa` branch, and prints `✓ ready to run nelsonctl!`. It does **not** build or run the pipeline — that's manual:

```bash
cd ~/build/nelsonctl-test
nelsonctl --verbose --no-pr specs/changes/guestbook-spa
```

For debug output:

```bash
NELSONCTL_DEBUG=1 nelsonctl --verbose --no-pr specs/changes/guestbook-spa
```

Flags: `--keep` skips the reset, `--change <name>` targets a different change, `--debug` enables debug logging.

## Test Project

`~/build/nelsonctl-test/` — a blank repo with only litespec artifacts (initial commit `fb27c76b592`):

```
specs/changes/guestbook-spa/
├── .litespec.yaml
├── proposal.md          ← motivation, scope, non-goals
├── design.md             ← Vite + vanilla TS, directory structure, data model, CSS vars
├── tasks.md              ← 3 phases, 18 unchecked tasks
└── specs/
    ├── project-setup/spec.md    ← Vite scaffold, strict TS, dev/build
    ├── app-skeleton/spec.md     ← CSS custom properties, responsive layout, header/main/footer
    └── guestbook/spec.md         ← GuestEntry type, localStorage, form, validation, rendering
```

Three phases:

| Phase | What the agent implements |
|-------|--------------------------|
| 1. Project Setup | Vite + vanilla TypeScript, strict config, semantic HTML |
| 2. App Skeleton | CSS custom properties, responsive layout, header/main/footer |
| 3. Guest Book | GuestEntry type, localStorage persistence, form with validation, entry list |

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

## Last Run Results

_No run yet against the guestbook-spa change._

## Known Issues

### 1. Agent doesn't reliably execute fix prompts

The fix step tells the agent what to do (e.g., "mark tasks as done"). The agent acknowledges and describes doing it, but sometimes doesn't actually make the change. Observed in the old `uppercase-greet` test — may or may not reproduce with the guestbook SPA.

**Possible fixes:**
- Controller calls `read_file` on `tasks.md` after each fix to verify the change was applied
- Stronger prompting ("You MUST use your edit tool to change tasks.md. Do not just describe the change.")
- Increase max attempts (3 → 5) to give more chances

### 2. Review severity calibration

The reviewer may flag unchecked tasks as CRITICAL. For `fail_on: critical`, this means a fully implemented change fails because the task list wasn't ticked. Whether this is correct depends on how strict we want to be.

**Possible fixes:**
- Tweak the `litespec-review` skill prompt to classify unchecked tasks as WARNING when code exists and passes tests
- Change `fail_on` to `warning` in config (weak — defeats the purpose of strictness)
- Have the controller discount task-tracking findings vs code-correctness findings

## Next Steps (Ordered)

### Step 1: Run against guestbook-spa and get a clean pass

Run the pipeline against the 3-phase guestbook-spa change:
- Phase 1 (Project Setup): agent scaffolds Vite + TS
- Phase 2 (App Skeleton): agent adds layout + CSS
- Phase 3 (Guest Book): agent implements the feature
- Final review passes
- Pipeline completes

### Step 2: Test with `--no-pr` off

Run without `--no-pr` to verify the PR creation step works end-to-end:
- Branch pushes to remote
- `gh pr create` succeeds
- PR body includes change summary

### Step 3: Test edge cases

- Phase where agent produces no changes (should fail review)
- Phase where the build fails (agent should fix)
- Phase where implementation already exists (idempotent behavior)

### Step 4: Remove `NELSONCTL_DEBUG` training wheels

The debug logging is useful but noisy. Replace with structured trace output to a file (the trace system already exists in `internal/trace/`). Make verbose mode useful without debug env var.

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
