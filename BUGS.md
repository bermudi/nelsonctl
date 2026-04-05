# Known Issues & Design Decisions

## Pi RPC Crash Recovery

**Decision:** On pi process crash mid-phase, restart pi and re-prompt the current phase from scratch. Do not attempt session recovery. Keep RPC for remaining phases.

**Rationale:** The agent edits files on disk, so partial work survives the crash. Re-prompting apply on a partially-applied phase is safe — the agent sees existing changes via git diff. Session transcript recovery (`--session`) adds complexity for a rare edge case. Clean restart guarantees no corrupted state.

**Trade-off:** Loses conversation context from the current phase. Acceptable because each phase is self-contained by design.
