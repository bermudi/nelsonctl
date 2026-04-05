package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	reviewState := filepath.Join(repoRoot, "review.count")

	mustWriteScript(t, filepath.Join(binDir, "opencode"), `#!/bin/sh
prompt=""
while [ $# -gt 0 ]; do
  case "$1" in
    --prompt|-p)
      shift
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
    count=0
    if [ -f "$MOCK_REVIEW_STATE" ]; then
      count=$(cat "$MOCK_REVIEW_STATE")
    fi
    count=$((count + 1))
    printf '%s' "$count" > "$MOCK_REVIEW_STATE"
    if [ "$count" -eq 1 ]; then
      echo "issues found: please fix"
    else
      echo "no issues found"
    fi
    ;;
  *)
    echo "applied changes"
    ;;
esac
`)
	mustWriteScript(t, filepath.Join(binDir, "git"), `#!/bin/sh
echo "git $*" >> "$MOCK_GIT_LOG"
`)
	mustWriteScript(t, filepath.Join(binDir, "gh"), `#!/bin/sh
echo "gh $*" >> "$MOCK_GH_LOG"
`)

	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+oldPath)
	t.Setenv("MOCK_AGENT_LOG", agentLog)
	t.Setenv("MOCK_GIT_LOG", gitLog)
	t.Setenv("MOCK_GH_LOG", ghLog)
	t.Setenv("MOCK_REVIEW_STATE", reviewState)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runCLI(context.Background(), []string{"--agent", "opencode", "--timeout", "1s", "--verbose", changeDir}, repoRoot, stdout, stderr)
	if code != 0 {
		t.Fatalf("runCLI() code = %d, stderr = %s", code, stderr.String())
	}

	out := stdout.String()
	for _, want := range []string{
		"branch: change/initial-scaffold",
		"phase 1: attempts=2 passed=true",
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
	for _, want := range []string{"litespec-apply skill", "The review found these issues:", "pre-archive mode"} {
		if !strings.Contains(agentLogData, want) {
			t.Fatalf("agent log missing %q in %q", want, agentLogData)
		}
	}

	gitLogData := mustReadFile(t, gitLog)
	for _, want := range []string{
		"git checkout -b change/initial-scaffold",
		"git add --",
		"git commit -m chore: add planning artifacts for initial-scaffold",
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
	t.Setenv("MOCK_AGENT_LOG", filepath.Join(repoRoot, "agent.log"))
	t.Setenv("MOCK_GIT_LOG", filepath.Join(repoRoot, "git.log"))

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runCLI(context.Background(), []string{"--dry-run", "--agent", "opencode", changeDir}, repoRoot, stdout, stderr)
	if code != 0 {
		t.Fatalf("runCLI() code = %d, stderr = %s", code, stderr.String())
	}

	out := stdout.String()
	for _, want := range []string{"Dry run for", "Branch: change/initial-scaffold", "Phase 1: Foundation"} {
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

	binDir := t.TempDir()
	gitLog := filepath.Join(repoRoot, "git.log")
	mustWriteScript(t, filepath.Join(binDir, "opencode"), `#!/bin/sh
echo "ok"
`)
	mustWriteScript(t, filepath.Join(binDir, "git"), `#!/bin/sh
echo "git $*" >> "$MOCK_GIT_LOG"
`)
	mustWriteScript(t, filepath.Join(binDir, "gh"), `#!/bin/sh
echo "gh $*" >> "$MOCK_GH_LOG"
`)
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+oldPath)
	t.Setenv("MOCK_GIT_LOG", gitLog)
	t.Setenv("MOCK_GH_LOG", filepath.Join(repoRoot, "gh.log"))

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runCLI(context.Background(), []string{"--no-pr", "--agent", "opencode", changeDir}, repoRoot, stdout, stderr)
	if code != 0 {
		t.Fatalf("runCLI() code = %d, stderr = %s", code, stderr.String())
	}

	if _, err := os.Stat(filepath.Join(repoRoot, "gh.log")); !os.IsNotExist(err) {
		t.Fatalf("gh log should not exist or be unused: %v", err)
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
