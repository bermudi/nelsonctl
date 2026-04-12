//go:build !short

package pipeline

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bermudi/nelsonctl/internal/agent"
	"github.com/bermudi/nelsonctl/internal/config"
	ctrl "github.com/bermudi/nelsonctl/internal/controller"
	"github.com/bermudi/nelsonctl/internal/git"
)

// e2eAgent writes real files to disk to simulate agent work.
type e2eAgent struct {
	repoDir  string
	phaseNum int
}

func (a *e2eAgent) Name() string                                 { return "e2e-fake" }
func (a *e2eAgent) CheckPrerequisites(ctx context.Context) error { return nil }
func (a *e2eAgent) Cleanup(ctx context.Context) error            { return nil }
func (a *e2eAgent) AsRPC() agent.RPCAgent                        { return nil }
func (a *e2eAgent) Events() <-chan agent.Event                   { return nil }

func (a *e2eAgent) ExecuteStep(ctx context.Context, step agent.Step, prompt, model string) (*agent.Result, error) {
	switch step {
	case agent.StepApply:
		a.phaseNum++
		switch a.phaseNum {
		case 1:
			// Modify tracked greet.go to add UpperGreet so git diff sees changes.
			if err := os.WriteFile(filepath.Join(a.repoDir, "greet.go"), []byte("package main\n\nimport (\n\t\"fmt\"\n\t\"strings\"\n)\n\nfunc greet(name string) string { return fmt.Sprintf(\"hello, %s\", name) }\n\nfunc UpperGreet(name string) string { return strings.ToUpper(greet(name)) }\n"), 0o644); err != nil {
				return nil, err
			}
		case 2:
			// Modify tracked main.go to add FormatGreet.
			if err := os.WriteFile(filepath.Join(a.repoDir, "main.go"), []byte("package main\n\nimport \"fmt\"\n\nfunc main() {}\n\nfunc FormatGreet(name string) string { return fmt.Sprintf(\"[%s]\", greet(name)) }\n"), 0o644); err != nil {
				return nil, err
			}
		}
		return &agent.Result{Stdout: fmt.Sprintf("implemented phase %d", a.phaseNum), ExitCode: 0}, nil
	case agent.StepReview, agent.StepFinalReview:
		return &agent.Result{Stdout: "no issues found", ExitCode: 0}, nil
	case agent.StepFix:
		return &agent.Result{Stdout: "fixed", ExitCode: 0}, nil
	case agent.StepCommit:
		// Actually commit using git.
		cmd := exec.CommandContext(ctx, "git", "add", "-A")
		cmd.Dir = a.repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("git add: %w: %s", err, out)
		}
		// Extract message from prompt after the colon
		msg := "commit"
		if idx := strings.LastIndex(prompt, "\n"); idx >= 0 {
			msg = strings.TrimSpace(prompt[idx+1:])
		}
		cmd = exec.CommandContext(ctx, "git", "commit", "-m", msg)
		cmd.Dir = a.repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			return &agent.Result{Stdout: string(out), ExitCode: 1}, nil
		}
		return &agent.Result{Stdout: "committed", ExitCode: 0}, nil
	}
	return &agent.Result{Stdout: "ok", ExitCode: 0}, nil
}

// controllerFn builds a fakeController that drives submit_prompt → run_review → approve.
func e2eController() *fakeController {
	return &fakeController{
		phaseFn: func(ctx context.Context, request ctrl.PhaseRequest, dispatcher ctrl.Dispatcher) (*ctrl.Result, error) {
			if _, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "1", Name: ctrl.ToolSubmitPrompt, Arguments: []byte(`{"prompt":"apply this phase"}`)}); err != nil {
				return nil, err
			}
			if _, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "2", Name: ctrl.ToolRunReview, Arguments: []byte(`{}`)}); err != nil {
				return nil, err
			}
			if _, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "3", Name: ctrl.ToolCommit, Arguments: []byte(fmt.Sprintf(`{"message":"feat(%s): complete phase %d - %s"}`, request.ChangeName, request.Phase.Number, request.Phase.Name))}); err != nil {
				return nil, err
			}
			approve, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "4", Name: ctrl.ToolApprove, Arguments: []byte(`{"summary":"phase passed"}`)})
			if err != nil {
				return nil, err
			}
			return &ctrl.Result{Summary: approve.Summary}, nil
		},
		finalFn: func(ctx context.Context, request ctrl.FinalReviewRequest, dispatcher ctrl.Dispatcher) (*ctrl.Result, error) {
			if _, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "5", Name: ctrl.ToolRunReview, Arguments: []byte(`{}`)}); err != nil {
				return nil, err
			}
			approve, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "6", Name: ctrl.ToolApprove, Arguments: []byte(`{"summary":"final passed"}`)})
			if err != nil {
				return nil, err
			}
			return &ctrl.Result{Summary: approve.Summary}, nil
		},
	}
}

func gitExec(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v: %s", strings.Join(args, " "), err, out)
	}
}

func gitLog(t *testing.T, dir string) []string {
	t.Helper()
	cmd := exec.Command("git", "log", "--oneline", "--format=%s")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git log failed: %v: %s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

func initRepo(t *testing.T) string {
	t.Helper()
	repoDir := t.TempDir()
	gitExec(t, repoDir, "init")
	gitExec(t, repoDir, "config", "user.email", "test@test.com")
	gitExec(t, repoDir, "config", "user.name", "Test")
	mustWriteFile(t, filepath.Join(repoDir, "main.go"), "package main\n\nfunc main() {}\n")
	mustWriteFile(t, filepath.Join(repoDir, "greet.go"), "package main\n\nimport \"fmt\"\n\nfunc greet(name string) string { return fmt.Sprintf(\"hello, %s\", name) }\n")
	gitExec(t, repoDir, "add", "--all")
	gitExec(t, repoDir, "commit", "-m", "initial commit")

	// Set up a local bare repo as "origin" so push succeeds without a network.
	bareDir := t.TempDir()
	gitExec(t, bareDir, "init", "--bare")
	gitExec(t, repoDir, "remote", "add", "origin", bareDir)
	// Push whatever the default branch is (could be main or master).
	cmd := exec.Command("git", "branch", "--show-current")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git branch --show-current: %v", err)
	}
	gitExec(t, repoDir, "push", "-u", "origin", strings.TrimSpace(string(out)))

	return repoDir
}

func TestE2ERealGitSinglePhase(t *testing.T) {
	repoDir := initRepo(t)

	changeDir := filepath.Join(repoDir, "specs", "changes", "uppercase-greet")
	mustWriteFile(t, filepath.Join(changeDir, "tasks.md"), "# Tasks\n\n## Phase 1: Add uppercase greet\n- [ ] Create upper.go\n- [ ] Add tests\n")
	mustWriteFile(t, filepath.Join(changeDir, "proposal.md"), "Add UpperGreet wrapping greet() with strings.ToUpper()\n")

	gitClient := git.NewClient(repoDir)
	agentClient := &e2eAgent{repoDir: repoDir}
	controller := e2eController()
	pr := &fakePR{note: "skipped"}

	p := &Pipeline{
		ChangePath:    changeDir,
		RepoDir:       repoDir,
		Agent:         agentClient,
		Controller:    controller,
		Git:           gitClient,
		PR:            pr,
		MaxAttempts:   3,
		Config:        config.Config{Review: config.ReviewConfig{FailOn: config.FailOnCritical}, Steps: config.DefaultConfig().Steps},
		processExists: func(pid int) bool { return false },
		now:           time.Now,
	}

	ctx := context.Background()
	report, err := p.Run(ctx)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(report.Phases) != 1 {
		t.Fatalf("expected 1 phase, got %d", len(report.Phases))
	}
	if !report.Phases[0].Passed {
		t.Fatalf("phase 1 did not pass: %+v", report.Phases[0])
	}
	if !report.FinalReviewPassed {
		t.Fatalf("final review did not pass")
	}
	if report.ChangeName != "uppercase-greet" {
		t.Fatalf("ChangeName = %q, want uppercase-greet", report.ChangeName)
	}
	if report.BranchName != "change/uppercase-greet" {
		t.Fatalf("BranchName = %q, want change/uppercase-greet", report.BranchName)
	}

	// Verify real git state
	branch, err := gitClient.CurrentBranch(ctx)
	if err != nil {
		t.Fatalf("CurrentBranch() error = %v", err)
	}
	if branch != "change/uppercase-greet" {
		t.Fatalf("current branch = %q, want change/uppercase-greet", branch)
	}

	commits := gitLog(t, repoDir)
	if len(commits) < 2 {
		t.Fatalf("expected at least 2 commits (initial + phase), got %d: %v", len(commits), commits)
	}
	var hasPhase bool
	for _, c := range commits {
		if strings.Contains(c, "feat(uppercase-greet): complete phase 1") {
			hasPhase = true
		}
	}
	if !hasPhase {
		t.Fatalf("missing phase commit in: %v", commits)
	}

	// Verify greet.go contains UpperGreet (modified by the agent).
	data, err := os.ReadFile(filepath.Join(repoDir, "greet.go"))
	if err != nil {
		t.Fatalf("read greet.go: %v", err)
	}
	if !strings.Contains(string(data), "UpperGreet") {
		t.Fatalf("greet.go missing UpperGreet:\n%s", data)
	}
}

func TestE2ERealGitMultiPhase(t *testing.T) {
	repoDir := initRepo(t)

	changeDir := filepath.Join(repoDir, "specs", "changes", "multi-feature")
	mustWriteFile(t, filepath.Join(changeDir, "tasks.md"),
		"# Tasks\n\n## Phase 1: Add uppercase greet\n- [ ] Create upper.go\n- [ ] Add tests\n\n## Phase 2: Add formatter\n- [ ] Create formatter.go\n")
	mustWriteFile(t, filepath.Join(changeDir, "proposal.md"), "Add UpperGreet and FormatGreet\n")

	gitClient := git.NewClient(repoDir)
	agentClient := &e2eAgent{repoDir: repoDir}
	controller := e2eController()
	pr := &fakePR{note: "skipped"}

	p := &Pipeline{
		ChangePath:    changeDir,
		RepoDir:       repoDir,
		Agent:         agentClient,
		Controller:    controller,
		Git:           gitClient,
		PR:            pr,
		MaxAttempts:   3,
		Config:        config.Config{Review: config.ReviewConfig{FailOn: config.FailOnCritical}, Steps: config.DefaultConfig().Steps},
		processExists: func(pid int) bool { return false },
		now:           time.Now,
	}

	ctx := context.Background()
	report, err := p.Run(ctx)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(report.Phases) != 2 {
		t.Fatalf("expected 2 phases, got %d", len(report.Phases))
	}
	for i, pr := range report.Phases {
		if !pr.Passed {
			t.Fatalf("phase %d did not pass: %+v", i+1, pr)
		}
	}
	if !report.FinalReviewPassed {
		t.Fatalf("final review did not pass")
	}
	if report.BranchName != "change/multi-feature" {
		t.Fatalf("BranchName = %q, want change/multi-feature", report.BranchName)
	}

	// Verify real git state
	branch, err := gitClient.CurrentBranch(ctx)
	if err != nil {
		t.Fatalf("CurrentBranch() error = %v", err)
	}
	if branch != "change/multi-feature" {
		t.Fatalf("current branch = %q, want change/multi-feature", branch)
	}

	commits := gitLog(t, repoDir)
	// initial + phase1 + phase2 = 3
	if len(commits) < 3 {
		t.Fatalf("expected at least 3 commits, got %d: %v", len(commits), commits)
	}
	var hasPhase1, hasPhase2 bool
	for _, c := range commits {
		if strings.Contains(c, "feat(multi-feature): complete phase 1") {
			hasPhase1 = true
		}
		if strings.Contains(c, "feat(multi-feature): complete phase 2") {
			hasPhase2 = true
		}
	}
	if !hasPhase1 {
		t.Fatalf("missing phase 1 commit in: %v", commits)
	}
	if !hasPhase2 {
		t.Fatalf("missing phase 2 commit in: %v", commits)
	}

	// Verify modified files contain expected functions.
	greetData, err := os.ReadFile(filepath.Join(repoDir, "greet.go"))
	if err != nil {
		t.Fatalf("read greet.go: %v", err)
	}
	if !strings.Contains(string(greetData), "UpperGreet") {
		t.Fatalf("greet.go missing UpperGreet:\n%s", greetData)
	}
	mainData, err := os.ReadFile(filepath.Join(repoDir, "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(mainData), "FormatGreet") {
		t.Fatalf("main.go missing FormatGreet:\n%s", mainData)
	}

	// Verify report fields
	if report.Phases[0].Phase.Name != "Add uppercase greet" {
		t.Fatalf("phase 1 name = %q", report.Phases[0].Phase.Name)
	}
	if report.Phases[1].Phase.Name != "Add formatter" {
		t.Fatalf("phase 2 name = %q", report.Phases[1].Phase.Name)
	}
	if report.TotalAttempts < 2 {
		t.Fatalf("TotalAttempts = %d, want >= 2", report.TotalAttempts)
	}
}
