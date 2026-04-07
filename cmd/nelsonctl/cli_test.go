package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bermudi/nelsonctl/internal/pipeline"
	"github.com/bermudi/nelsonctl/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

func TestRunCLIWithMockAgent(t *testing.T) {
	repoRoot := t.TempDir()
	changeDir := filepath.Join(repoRoot, "specs", "changes", "initial-scaffold")
	mustWriteFile(t, filepath.Join(changeDir, "tasks.md"), "# Tasks\n\n## Phase 1: Foundation\n- [ ] Initialize Go module\n\n## Phase 2: Adapter\n- [ ] Wire adapter\n")
	mustWriteFile(t, filepath.Join(changeDir, "proposal.md"), "proposal\n")

	binDir := t.TempDir()
	agentLog := filepath.Join(repoRoot, "agent.log")
	gitLog := filepath.Join(repoRoot, "git.log")
	ghLog := filepath.Join(repoRoot, "gh.log")
	configHome := t.TempDir()
	mustWriteFile(t, filepath.Join(configHome, "nelsonctl", "config.yaml"), strings.Join([]string{
		"agent: opencode",
		"steps:",
		"  apply:",
		"    model: minimax/minimax-m2.7",
		"    timeout: 1s",
		"  review:",
		"    model: moonshotai/kimi-k2.5",
		"    timeout: 1s",
		"  fix:",
		"    model: minimax/minimax-m2.7",
		"    timeout: 1s",
		"controller:",
		"  provider: openrouter",
		"  model: deepseek/deepseek-reasoner",
		"  max_tool_calls: 50",
		"  timeout: 45m",
		"review:",
		"  fail_on: critical",
	}, "\n"))
	mustWriteFile(t, filepath.Join(repoRoot, ".agents", "skills", "litespec-apply", "SKILL.md"), "apply")
	mustWriteFile(t, filepath.Join(repoRoot, ".agents", "skills", "litespec-review", "SKILL.md"), "review")

	mustWriteScript(t, filepath.Join(binDir, "opencode"), `#!/bin/sh
prompt=""
while [ $# -gt 0 ]; do
  case "$1" in
    -p)
      shift; prompt="$1"; shift; continue
      ;;
    --format)
      shift; shift; continue
      ;;
    --allowedTools)
      shift; shift; continue
      ;;
    --output-format)
      shift; shift; continue
      ;;
    --prompt)
      shift; prompt="$1"; shift; continue
      ;;
    --json|--execute|--stream-json)
      shift; continue
      ;;
    -*)
      shift; continue
      ;;
    *)
      prompt="$1"
      ;;
  esac
  shift
done
printf '%s\n' "$prompt" >> "$MOCK_AGENT_LOG"
case "$prompt" in
  *"pre-archive mode"*)
    echo "final review: no issues found"
    ;;
  *"The review found these issues:"*)
    echo "fixed after review"
    ;;
  *"litespec-review skill"*)
    echo "no issues found"
    ;;
  *)
    echo "applied changes"
    ;;
esac
`)
	mustWriteScript(t, filepath.Join(binDir, "git"), `#!/bin/sh
case "$*" in
  "rev-parse --verify "*) exit 1 ;;
  "diff --cached --quiet") exit 1 ;;
esac
echo "git $*" >> "$MOCK_GIT_LOG"
`)
	mustWriteScript(t, filepath.Join(binDir, "gh"), `#!/bin/sh
echo "gh $*" >> "$MOCK_GH_LOG"
`)

	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+oldPath)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Setenv("MOCK_AGENT_LOG", agentLog)
	t.Setenv("MOCK_GIT_LOG", gitLog)
	t.Setenv("MOCK_GH_LOG", ghLog)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runCLI(context.Background(), []string{"--verbose", changeDir}, repoRoot, strings.NewReader(""), stdout, stderr)
	if code != 0 {
		t.Fatalf("runCLI() code = %d, stdout = %s, stderr = %s", code, stdout.String(), stderr.String())
	}

	out := stdout.String()
	for _, want := range []string{
		"branch: change/initial-scaffold",
		"phase 1: attempts=1 passed=true",
		"phase 2: attempts=",
		"final review passed: true",
		"applied changes",
		"no issues found",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q in %q", want, out)
		}
	}

	agentLogData := mustReadFile(t, agentLog)
	for _, want := range []string{"litespec-apply skill", "litespec-review skill", "pre-archive mode"} {
		if !strings.Contains(agentLogData, want) {
			t.Fatalf("agent log missing %q in %q", want, agentLogData)
		}
	}

	gitLogData := mustReadFile(t, gitLog)
	for _, want := range []string{
		"git checkout -b change/initial-scaffold",
		"git add --all",
		"git commit -m chore: add litespec artifacts for initial-scaffold",
		"git push -u origin change/initial-scaffold",
	} {
		if !strings.Contains(gitLogData, want) {
			t.Fatalf("git log missing %q in %q", want, gitLogData)
		}
	}

	ghLogData := mustReadFile(t, ghLog)
	if !strings.Contains(ghLogData, "gh pr create --title initial-scaffold --body-file") {
		t.Fatalf("gh log missing pr create: %q", ghLogData)
	}
}

func TestRunCLIDryRunSkipsExecution(t *testing.T) {
	repoRoot := t.TempDir()
	changeDir := filepath.Join(repoRoot, "specs", "changes", "initial-scaffold")
	mustWriteFile(t, filepath.Join(changeDir, "tasks.md"), "# Tasks\n\n## Phase 1: Foundation\n- [ ] Initialize Go module\n")
	mustWriteFile(t, filepath.Join(repoRoot, ".agents", "skills", "litespec-apply", "SKILL.md"), "apply")
	mustWriteFile(t, filepath.Join(repoRoot, ".agents", "skills", "litespec-review", "SKILL.md"), "review")
	configHome := t.TempDir()
	mustWriteFile(t, filepath.Join(configHome, "nelsonctl", "config.yaml"), strings.Join([]string{
		"agent: opencode",
		"steps:",
		"  apply:",
		"    model: minimax/minimax-m2.7",
		"    timeout: 30m",
		"  review:",
		"    model: moonshotai/kimi-k2.5",
		"    timeout: 15m",
		"  fix:",
		"    model: minimax/minimax-m2.7",
		"    timeout: 30m",
		"controller:",
		"  provider: openrouter",
		"  model: deepseek/deepseek-reasoner",
		"  max_tool_calls: 50",
		"  timeout: 45m",
		"review:",
		"  fail_on: critical",
	}, "\n"))

	binDir := t.TempDir()
	mustWriteScript(t, filepath.Join(binDir, "opencode"), `#!/bin/sh
echo "unexpected agent invocation" >> "$MOCK_AGENT_LOG"
exit 99
`)
	mustWriteScript(t, filepath.Join(binDir, "git"), `#!/bin/sh
echo "unexpected git invocation" >> "$MOCK_GIT_LOG"
exit 99
`)
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+oldPath)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Setenv("MOCK_AGENT_LOG", filepath.Join(repoRoot, "agent.log"))
	t.Setenv("MOCK_GIT_LOG", filepath.Join(repoRoot, "git.log"))

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runCLI(context.Background(), []string{"--dry-run", changeDir}, repoRoot, strings.NewReader(""), stdout, stderr)
	if code != 0 {
		t.Fatalf("runCLI() code = %d, stdout = %s, stderr = %s", code, stdout.String(), stderr.String())
	}

	out := stdout.String()
	for _, want := range []string{"Dry run for", "Mode: Ralph", "Agent: opencode", "Review fail_on: critical", "Branch: change/initial-scaffold", "Phase 1: Foundation"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q in %q", want, out)
		}
	}

	if _, err := os.Stat(filepath.Join(repoRoot, "agent.log")); !os.IsNotExist(err) {
		t.Fatalf("agent log should not exist or be unused: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "git.log")); !os.IsNotExist(err) {
		t.Fatalf("git log should not exist or be unused: %v", err)
	}
}

func TestRunCLINoPRSkipsPRCreation(t *testing.T) {
	repoRoot := t.TempDir()
	changeDir := filepath.Join(repoRoot, "specs", "changes", "initial-scaffold")
	mustWriteFile(t, filepath.Join(changeDir, "tasks.md"), "# Tasks\n\n## Phase 1: Foundation\n- [ ] Initialize Go module\n")
	mustWriteFile(t, filepath.Join(changeDir, "proposal.md"), "proposal\n")
	mustWriteFile(t, filepath.Join(repoRoot, ".agents", "skills", "litespec-apply", "SKILL.md"), "apply")
	mustWriteFile(t, filepath.Join(repoRoot, ".agents", "skills", "litespec-review", "SKILL.md"), "review")
	configHome := t.TempDir()
	mustWriteFile(t, filepath.Join(configHome, "nelsonctl", "config.yaml"), strings.Join([]string{
		"agent: opencode",
		"steps:",
		"  apply:",
		"    model: minimax/minimax-m2.7",
		"    timeout: 30m",
		"  review:",
		"    model: moonshotai/kimi-k2.5",
		"    timeout: 15m",
		"  fix:",
		"    model: minimax/minimax-m2.7",
		"    timeout: 30m",
		"controller:",
		"  provider: openrouter",
		"  model: deepseek/deepseek-reasoner",
		"  max_tool_calls: 50",
		"  timeout: 45m",
		"review:",
		"  fail_on: critical",
	}, "\n"))

	binDir := t.TempDir()
	gitLog := filepath.Join(repoRoot, "git.log")
	mustWriteScript(t, filepath.Join(binDir, "opencode"), `#!/bin/sh
echo "ok"
`)
	mustWriteScript(t, filepath.Join(binDir, "git"), `#!/bin/sh
case "$*" in
  "rev-parse --verify "*) exit 1 ;;
  "diff --cached --quiet") exit 1 ;;
esac
echo "git $*" >> "$MOCK_GIT_LOG"
`)
	mustWriteScript(t, filepath.Join(binDir, "gh"), `#!/bin/sh
echo "gh $*" >> "$MOCK_GH_LOG"
`)
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+oldPath)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Setenv("MOCK_GIT_LOG", gitLog)
	t.Setenv("MOCK_GH_LOG", filepath.Join(repoRoot, "gh.log"))

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runCLI(context.Background(), []string{"--no-pr", "--verbose", changeDir}, repoRoot, strings.NewReader(""), stdout, stderr)
	if code != 0 {
		t.Fatalf("runCLI() code = %d, stdout = %s, stderr = %s", code, stdout.String(), stderr.String())
	}

	if _, err := os.Stat(filepath.Join(repoRoot, "gh.log")); !os.IsNotExist(err) {
		t.Fatalf("gh log should not exist or be unused: %v", err)
	}
}

func TestRunCLIInitWritesConfig(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	code := runCLI(context.Background(), []string{"init"}, t.TempDir(), strings.NewReader("\n"), stdout, stderr)
	if code != 0 {
		t.Fatalf("runCLI() code = %d, stdout = %s, stderr = %s", code, stdout.String(), stderr.String())
	}

	configPath := filepath.Join(configHome, "nelsonctl", "config.yaml")
	data := mustReadFile(t, configPath)
	for _, want := range []string{
		"agent: pi",
		"provider: openrouter",
		"model: deepseek/deepseek-reasoner",
		"model: minimax/minimax-m2.7",
		"model: moonshotai/kimi-k2.5",
	} {
		if !strings.Contains(data, want) {
			t.Fatalf("config missing %q in %q", want, data)
		}
	}
	if strings.Contains(data, "API_KEY") {
		t.Fatalf("config should not contain credentials: %q", data)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustWriteScript(t *testing.T, path, content string) {
	t.Helper()
	mustWriteFile(t, path, content)
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestToTeaMsgBridgesAllEventTypes(t *testing.T) {
	tests := []struct {
		name  string
		event pipeline.Event
		want  tea.Msg
	}{
		{
			name:  "state event",
			event: pipeline.StateEvent{State: pipeline.StatePhaseLoop},
			want:  tui.StateMsg{State: pipeline.StatePhaseLoop},
		},
		{
			name:  "phase start event",
			event: pipeline.PhaseStartEvent{Number: 1, Name: "Foundation", Attempt: 2},
			want:  tui.PhaseMsg{Number: 1, Name: "Foundation", Attempt: 2},
		},
		{
			name:  "phase result event",
			event: pipeline.PhaseResultEvent{Number: 1, Passed: true, Attempts: 2, Review: "ok"},
			want:  tui.PhaseResultMsg{Number: 1, Passed: true, Attempts: 2, Review: "ok"},
		},
		{
			name:  "output event",
			event: pipeline.OutputEvent{Chunk: "hello"},
			want:  tui.OutputMsg{Chunk: "hello"},
		},
		{
			name:  "taunt event",
			event: pipeline.TauntEvent{PhaseNumber: 3},
			want:  tui.TauntMsg{PhaseNumber: 3},
		},
		{
			name:  "summary event",
			event: pipeline.SummaryEvent{PhasesCompleted: 2, PhasesFailed: 1, Duration: "1m30s", Branch: "change/foo"},
			want:  tui.SummaryMsg{PhasesCompleted: 2, PhasesFailed: 1, Duration: 90 * time.Second, Branch: "change/foo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toTeaMsg(tt.event)
			if got != tt.want {
				t.Fatalf("toTeaMsg() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
