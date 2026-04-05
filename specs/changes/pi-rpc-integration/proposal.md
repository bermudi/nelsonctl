# pi-rpc-integration

## Motivation

The original `initial-scaffold` change keeps nelsonctl simple by treating every apply, review, and fix step as a fresh CLI invocation. That is a good foundation, but it throws away implementation context between retries, makes review result detection overly dependent on raw text matching, and prevents step-specific model selection.

This change upgrades nelsonctl to a Pi-first architecture that can keep an implementation session alive across a whole phase while still preserving an independent reviewer. It also adds the missing operator ergonomics around configuration, resumability, and safety so the tool can recover from partial runs without turning into a chat app or a generic model router.

## Scope

- Add a two-tier agent system: a long-lived Pi RPC adapter for the smart path and the existing CLI adapters as an explicit fallback path.
- Introduce Pi session lifecycle rules: one apply/fix session per change run, a fresh disposable review session per review pass, and per-step model selection.
- Add configuration at `~/.config/nelsonctl/config.yaml` plus `nelsonctl init` for first-run setup, per-step timeouts, controller provider/model selection, and review failure policy.
- Replace the scaffold's regex-only review detection with a three-tier review extraction pipeline: fuzzy structured parsing, heuristic fallback, and optional controller-AI summarization.
- Add resume and safety behavior: validate the workspace and required skills, lock a change while it is running, scope git staging to changed files, recover from Pi crashes by restarting the current phase, and print a richer dry-run plan.
- Extend the TUI so the operator can see whether the run is in Nelson mode or Ralph mode, which model is active, and whether execution resumed from partially completed work.

## Non-Goals

- Not removing the existing CLI adapters. Non-Pi agents remain supported when the user explicitly selects them.
- Not turning nelsonctl into a general-purpose model gateway. The controller AI is only for ambiguous review summarization.
- Not implementing transcript or session restoration after a Pi crash. Recovery restarts the current phase from disk.
- Not changing litespec itself or the one-change-per-run workflow established by `initial-scaffold`.
- Not introducing a TypeScript bridge, embedding the Pi SDK directly, or otherwise moving this integration out of Go.
