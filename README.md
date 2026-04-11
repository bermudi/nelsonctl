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
- OpenRouter controller configuration with a ready-to-use reasoning model
- Step-specific apply/review/fix models and timeouts
- `review.fail_on`

## Configuration overview

There are two separate AI configurations in `~/.config/nelsonctl/config.yaml`:

- `agent` + `steps.*` configure the **coding agent** that edits code.
- `controller.*` configures the **controller** that drives the apply/review/fix loop.

Supported controller providers are `deepseek`, `openrouter`, `opencode`, `poe`, and `poe-responses`.

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
  provider: openrouter
  model: deepseek/deepseek-reasoner
  max_tool_calls: 50
  timeout: 45m
review:
  fail_on: critical
```

Credentials stay in environment variables and are never written into `config.yaml`.

## Controller credentials

- `DEEPSEEK_API_KEY` for `controller.provider: deepseek`
- `OPENROUTER_API_KEY` for `controller.provider: openrouter`
- `OPENROUTER_API_KEY` for `controller.provider: opencode`
- `POE_API_KEY` or `POE_OAUTH_TOKEN` for `controller.provider: poe` and `controller.provider: poe-responses`

### Poe controller provider

Two Poe protocols are available:

| Provider | API | Endpoint | Auth header |
| --- | --- | --- | --- |
| `poe` | Chat Completions (OpenAI-compatible) | `https://api.poe.com/v1/chat/completions` | `Authorization: Bearer ...` |
| `poe-responses` | Responses (Poe native, recommended) | `https://api.poe.com/bot/{model}` | `Poe-API-Key: ...` |

#### Chat Completions (`poe`)

OpenAI-compatible, best for drop-in use with models that support it:

```yaml
controller:
  provider: poe
  model: claude-3-5-sonnet
  max_tool_calls: 50
  timeout: 45m
```

#### Responses API (`poe-responses`, recommended)

Poe's native API. Sends `system_instruction`, `query`/`messages`, `tools`, `tool_calls`, and `tool_results` directly. The model name is embedded in the endpoint URL. Supports tool calling and multi-turn conversations:

```yaml
controller:
  provider: poe-responses
  model: claude-3-5-sonnet
  max_tool_calls: 50
  timeout: 45m
```

#### Authentication (both protocols)

- API key: export `POE_API_KEY=poe-...`
- OAuth: exchange a Poe PKCE auth code for an access token, then export it as `POE_OAUTH_TOKEN`
- `POE_OAUTH_ACCESS_TOKEN` is also accepted as a fallback alias

## Coding agent configuration

`steps.apply.model`, `steps.review.model`, and `steps.fix.model` are passed directly to the selected coding agent, so the expected format depends on `agent`:

| Agent | nelsonctl field | CLI flag | Expected value format |
| --- | --- | --- | --- |
| `pi` | `steps.*.model` | `--model` | `provider/id` or pattern, optionally `:thinking` |
| `opencode` | `steps.*.model` | `--model` | `provider/model` |
| `claude` | `steps.*.model` | `--model` | model alias or full model name |
| `codex` | `steps.*.model` | `--model` | model id accepted by `codex exec --model` |
| `amp` | `steps.*.model` | `--mode` | one of `deep`, `large`, `rush`, `smart` |

These formats were checked against each agent's current `--help` output.

### Agent-specific notes

- `pi`: `pi --help` documents `--model` as `provider/id` or a fuzzy pattern, with optional `:thinking` suffix.
- `opencode`: `opencode run --help` documents `--model` as `provider/model`.
- `claude`: `claude --help` documents `--model` as an alias like `sonnet` or a full model name like `claude-sonnet-4-6`.
- `amp`: `amp --help` uses `-m, --mode`, not `--model`. Nelsonctl maps `steps.*.model` to Amp's mode flag.

If you use `opencode` with Poe as the provider, authenticate opencode itself first, typically with `opencode providers login --provider poe`, and select the OAuth method in the prompt.

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

## Testing

### Unit and integration tests

```bash
go test ./...
```

### E2E tests with real git (fast, no API keys)

The `TestE2ERealGit*` tests in `internal/pipeline/e2e_test.go` run the full pipeline state machine against a real git repo in a temp directory with stub agent/controller. They verify branch creation, commit messages, file mutations, and report fields — no network, no API keys, runs in ~0.2s.

```bash
go test ./internal/pipeline/ -run TestE2E -count=1 -v
```

Covers single-phase and multi-phase scenarios with real `git init`, `git commit`, and `git push` (to a local bare remote).

### Live E2E with real agent/controller

`e2e.sh` drives the full pipeline against `~/build/nelsonctl-test/` with the real configured agent and controller. Costs API calls but tests everything end-to-end.

```bash
bash e2e.sh              # reset test repo + build + run + check
bash e2e.sh --keep       # skip repo reset (re-run after a code fix)
bash e2e.sh --debug      # with NELSONCTL_DEBUG=1
bash e2e.sh --change foo # different change dir
```

Results are machine-readable: exit 0 = pass, exit 1 = fail. Logs are written to `e2e-logs/` with a `latest` symlink:

```bash
echo $?              # check result
cat e2e-logs/latest  # read the full output
```

## Notes

- Run `nelsonctl` from the repository root.
- The change path should point at a litespec change directory such as `specs/changes/add-dark-mode`.
- Dry-run prints the resolved mode, agent, step model/mode values, resume state, and remaining phase tasks without creating a lock or branch.
