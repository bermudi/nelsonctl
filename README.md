# nelsonctl

`nelsonctl` automates the litespec apply -> review -> fix loop for a single change directory.

## Usage

```bash
nelsonctl specs/changes/initial-scaffold
```

## Pi-first setup

`nelsonctl` defaults to Nelson mode when `pi` is installed and no explicit CLI agent override is provided.

1. Install `pi` and confirm `pi --mode rpc --no-extensions --version` works.
2. Run `nelsonctl init` to create `~/.config/nelsonctl/config.yaml`.
3. Export the controller credential for your configured provider.

Minimal setup writes a Pi-first config with:

- `agent: pi`
- DeepSeek or OpenRouter controller configuration
- Step-specific apply/review/fix models and timeouts
- `review.fail_on`

## Controller configuration

The controller is configured in `~/.config/nelsonctl/config.yaml`.

```yaml
agent: pi
steps:
  apply:
    model: minimax/minimax-m2.7
    timeout: 30m
  review:
    model: moonshotai/kimi-k2.5
    timeout: 15m
  fix:
    model: minimax/minimax-m2.7
    timeout: 30m
controller:
  provider: deepseek
  model: deepseek-reasoner
  max_tool_calls: 50
  timeout: 45m
review:
  fail_on: critical
```

Credentials stay in environment variables and are never written into `config.yaml`.

## Environment variables

- `DEEPSEEK_API_KEY` for `controller.provider: deepseek`
- `OPENROUTER_API_KEY` for `controller.provider: openrouter`

## Flags

- `--agent opencode|claude|codex|amp` — explicitly use a CLI agent instead of Pi
- `--dry-run` — print the execution plan without running anything
- `--no-pr` — skip PR creation after the pipeline completes
- `--verbose` — stream full agent output directly to the terminal instead of the TUI

## Examples

Dry run remaining phases:

```bash
nelsonctl --dry-run specs/changes/initial-scaffold
```

Use a CLI fallback agent and skip PR creation:

```bash
nelsonctl --agent claude --no-pr specs/changes/initial-scaffold
```

Initialize config interactively:

```bash
nelsonctl init
```

## What it does

1. Reuses or creates `change/<change-name>`
2. Resumes from the first unchecked phase in `tasks.md`
3. Runs each remaining phase through the controller-driven apply/review/fix loop
4. Runs a fresh final pre-archive review
5. Pushes the branch and opens a PR unless `--no-pr` is set

## CLI fallback

CLI agents remain supported when you explicitly select one with `--agent` or config. In that case `nelsonctl` runs in Ralph mode and still uses the controller loop, but agent execution happens via shell-out instead of Pi RPC sessions.

## Notes

- Run `nelsonctl` from the repository root.
- The change path should point at a litespec change directory such as `specs/changes/add-dark-mode`.
- Dry-run prints the resolved mode, agent, models, resume state, and remaining phase tasks without creating a lock or branch.
