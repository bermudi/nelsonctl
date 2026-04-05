---
name: litespec-archive
description: Validate and archive a completed change, applying delta operations to merge specs. Use when a change is done and the user wants to finalize it or says "archive".
---

Run `litespec validate <name>` to verify the change.

Review validation output. If errors exist, fix them before proceeding.

Run `litespec archive <name>` to apply delta operations and move to archive.

The CLI handles: RENAMED → REMOVED → MODIFIED → ADDED delta merge, then moves to archive/.

Optionally offer to create a branch and PR for the completed change.
