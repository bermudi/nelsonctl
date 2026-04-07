# pi-rpc-integration

## Motivation

The original `initial-scaffold` change keeps nelsonctl simple by treating every apply, review, and fix step as a fresh CLI invocation. That is a good foundation, but it throws away implementation context between retries, makes review result detection overly dependent on raw text matching, and prevents step-specific model selection.

More fundamentally, the execution loop is dumb. It applies a fixed prompt, regex-matches the review output for pass/fail signals, and feeds raw review text back as a fix prompt. It cannot reason about why something failed, whether a fix strategy is working, or how to adjust its approach across retries.

This change upgrades nelsonctl with two core capabilities:

1. A **controller AI** that drives the implementation loop. Instead of hardcoded prompts and string-matching review detection, the controller reads the specs, drafts rich apply prompts, analyzes review output through comprehension, crafts targeted fix strategies, and decides pass/fail — all through a tool-calling agent loop with a constrained tool set. The controller uses DeepSeek 3.2 reasoning (cheap, fast, smart enough) and works in both Nelson and Ralph modes.

2. A **Pi RPC adapter** that keeps an implementation session alive across a whole phase while preserving an independent reviewer. Combined with the controller, this gives nelsonctl persistent implementation context steered by AI-driven fix strategies.

The controller replaces the scaffold's regex-only review detection, the hardcoded prompt templates, and the planned three-tier review parser with a single coherent AI component. The pipeline becomes a mechanical state machine (branch, commit, lock, resume) that defers all reasoning to the controller.

## Scope

- Add a controller AI component (`internal/controller/`) that runs as a tool-calling agent loop per phase. The controller gets five tools: `read_file`, `get_diff`, `submit_prompt`, `run_review`, and `approve`. It drafts apply prompts, analyzes reviews, crafts fix prompts, and decides pass/fail through comprehension rather than parsing.
- Support DeepSeek direct API and OpenRouter as controller providers, both using OpenAI-compatible tool-calling endpoints. Model and provider are configurable.
- Add a two-tier agent system: a long-lived Pi RPC adapter for the smart path and the existing CLI adapters as an explicit fallback path. The controller works with both.
- Introduce Pi session lifecycle rules: one apply/fix session per change run, a fresh disposable review session per review pass, and per-step model selection.
- Add configuration at `~/.config/nelsonctl/config.yaml` plus `nelsonctl init` for first-run setup, per-step timeouts, controller provider/model selection, and review failure policy.
- Inject `review.fail_on` threshold into the controller's system prompt so the controller applies severity policy through reasoning, not code.
- Add resume and safety behavior: validate the workspace and required skills, lock a change while it is running, scope git staging to changed files, recover from Pi crashes by restarting the current phase, and print a richer dry-run plan.
- Surface controller activity in the TUI as status lines generated mechanically from tool calls. Full controller reasoning goes to the trace log.
- Enforce guardrails: max 50 tool calls and 45-minute timeout per controller conversation. Pipeline injects attempt count after failed reviews.

## Non-Goals

- Not giving the controller autonomy to restart phases with a different approach, decompose phases, or reorder tasks. The controller is a fix strategist, not a project manager.
- Not having the controller draft or bias the review prompt. Review stays mechanical (litespec-review skill, cold read). The controller only analyzes the output.
- Not having the controller see the implementation agent's transcript. It works from `get_diff` and `run_review` output only.
- Not removing the existing CLI adapters. Non-Pi agents remain supported when the user explicitly selects them.
- Not turning nelsonctl into a general-purpose model gateway. The controller AI is scoped to implementation loop orchestration.
- Not implementing transcript or session restoration after a Pi crash. Recovery restarts the current phase from disk.
- Not changing litespec itself or the one-change-per-run workflow established by `initial-scaffold`.
- Not introducing a TypeScript bridge, embedding the Pi SDK directly, or otherwise moving this integration out of Go.
