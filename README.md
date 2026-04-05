# nelsonctl

`nelsonctl` automates the litespec apply → review → fix loop for a single change directory.

## Usage

```bash
nelsonctl specs/changes/initial-scaffold
```

## Flags

- `--agent opencode|claude|codex|amp` — choose the agent CLI to use
- `--timeout 10m` — set the per-agent timeout
- `--dry-run` — print the execution plan without running anything
- `--no-pr` — skip PR creation after the pipeline completes
- `--verbose` — stream full agent output to the terminal

## Examples

Dry run:

```bash
nelsonctl --dry-run specs/changes/initial-scaffold
```

Use a different agent and skip the PR step:

```bash
nelsonctl --agent claude --no-pr specs/changes/initial-scaffold
```

## What it does

1. Creates a branch named `change/<change-name>`
2. Commits the planning artifacts
3. Runs each phase in `tasks.md` through apply/review/fix
4. Runs a final review
5. Pushes the branch and opens a PR, unless `--no-pr` is set

## Notes

- Run `nelsonctl` from the repository root.
- The change path should point at a litespec change directory such as `specs/changes/add-dark-mode`.
