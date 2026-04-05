package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/bermudi/nelsonctl/internal/agent"
)

type fakeAgent struct {
	calls   []string
	results []agent.Result
	errs    []error
}

func (f *fakeAgent) Name() string     { return "fake" }
func (f *fakeAgent) Available() error { return nil }

func (f *fakeAgent) Run(ctx context.Context, prompt string, workDir string) (*agent.Result, error) {
	f.calls = append(f.calls, prompt+"|"+workDir)
	idx := len(f.calls) - 1
	if idx < len(f.errs) && f.errs[idx] != nil {
		var res *agent.Result
		if idx < len(f.results) {
			res = &f.results[idx]
		}
		return res, f.errs[idx]
	}
	if idx < len(f.results) {
		res := f.results[idx]
		return &res, nil
	}
	return &agent.Result{Stdout: "ok", ExitCode: 0}, nil
}

type fakeGit struct {
	calls []string
}

func (f *fakeGit) CreateBranch(ctx context.Context, branch string) error {
	f.calls = append(f.calls, "branch:"+branch)
	return nil
}

func (f *fakeGit) Add(ctx context.Context, paths ...string) error {
	f.calls = append(f.calls, "add:"+strings.Join(paths, ","))
	return nil
}

func (f *fakeGit) Commit(ctx context.Context, subject, body string) error {
	f.calls = append(f.calls, "commit:"+subject+"|"+body)
	return nil
}

func (f *fakeGit) Push(ctx context.Context, remote, branch string, setUpstream bool) error {
	f.calls = append(f.calls, "push:"+remote+"|"+branch+"|"+strconv.FormatBool(setUpstream))
	return nil
}

type fakePR struct {
	call string
	note string
}

func (f *fakePR) Create(ctx context.Context, repoDir, title, bodyFile string) (string, error) {
	f.call = repoDir + "|" + title + "|" + bodyFile
	return f.note, nil
}

func TestPipelineRunTransitionsAndRetries(t *testing.T) {
	tmp := t.TempDir()
	changeDir := filepath.Join(tmp, "initial-scaffold")
	if err := writeFile(filepath.Join(changeDir, "tasks.md"), "# Tasks\n\n## Phase 1: Foundation\n- [ ] Task one\n\n## Phase 2: Adapter\n- [ ] Task two\n"); err != nil {
		t.Fatalf("write tasks: %v", err)
	}
	if err := writeFile(filepath.Join(changeDir, "proposal.md"), "proposal body\n"); err != nil {
		t.Fatalf("write proposal: %v", err)
	}

	agent := &fakeAgent{results: []agent.Result{
		{Stdout: "applied phase 1"},
		{Stdout: "issues found: please fix"},
		{Stdout: "applied phase 1 fix"},
		{Stdout: "no issues found"},
		{Stdout: "applied phase 2"},
		{Stdout: "looks good"},
		{Stdout: "final review: no issues found"},
	}}
	git := &fakeGit{}
	pr := &fakePR{note: "gh unavailable; manual instructions"}

	p := &Pipeline{ChangePath: changeDir, Agent: agent, Git: git, PR: pr, MaxAttempts: 3}
	report, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	wantStates := []State{StateInit, StateBranch, StateCommitArtifacts, StatePhaseLoop, StateFinalReview, StatePR, StateDone}
	if !reflect.DeepEqual(report.States, wantStates) {
		t.Fatalf("States = %#v, want %#v", report.States, wantStates)
	}

	if report.ChangeName != "initial-scaffold" {
		t.Fatalf("ChangeName = %q, want initial-scaffold", report.ChangeName)
	}
	if report.BranchName != "change/initial-scaffold" {
		t.Fatalf("BranchName = %q, want change/initial-scaffold", report.BranchName)
	}
	if len(report.Phases) != 2 {
		t.Fatalf("len(Phases) = %d, want 2", len(report.Phases))
	}
	if got, want := report.Phases[0].Attempts, 2; got != want {
		t.Fatalf("phase1 Attempts = %d, want %d", got, want)
	}
	if !report.Phases[0].Passed {
		t.Fatalf("phase1 Passed = false, want true")
	}
	if got, want := report.Phases[1].Attempts, 1; got != want {
		t.Fatalf("phase2 Attempts = %d, want %d", got, want)
	}
	if !report.Phases[1].Passed {
		t.Fatalf("phase2 Passed = false, want true")
	}
	if !report.FinalReviewPassed {
		t.Fatalf("FinalReviewPassed = false, want true")
	}
	if pr.call != changeDir+"|initial-scaffold|"+filepath.Join(changeDir, "proposal.md") {
		t.Fatalf("PR call = %q", pr.call)
	}

	wantGitCalls := []string{
		"branch:change/initial-scaffold",
		"add:" + changeDir,
		"commit:chore: add planning artifacts for initial-scaffold|Planning artifacts for initial-scaffold\n\nPhase 1: Foundation\nPhase 2: Adapter",
		"add:" + changeDir,
		"commit:chore(phase-1): Foundation|Phase 1: Foundation\n- Task one",
		"add:" + changeDir,
		"commit:chore(phase-2): Adapter|Phase 2: Adapter\n- Task two",
		"push:origin|change/initial-scaffold|true",
	}
	if !reflect.DeepEqual(git.calls, wantGitCalls) {
		t.Fatalf("git.calls = %#v, want %#v", git.calls, wantGitCalls)
	}

	if got, want := len(agent.calls), 7; got != want {
		t.Fatalf("len(agent.calls) = %d, want %d", got, want)
	}
	if !strings.Contains(agent.calls[0], "litespec-apply") || !strings.Contains(agent.calls[1], "litespec-review") {
		t.Fatalf("agent prompts not constructed as expected: %#v", agent.calls[:2])
	}
}

func TestReviewPassed(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		exitCode int
		want     bool
	}{
		{name: "failure output", output: "issues found: fix this", exitCode: 0, want: false},
		{name: "non-zero exit", output: "anything", exitCode: 1, want: false},
		{name: "ambiguous pass", output: "looks fine", exitCode: 0, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ReviewPassed(tt.output, tt.exitCode); got != tt.want {
				t.Fatalf("ReviewPassed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPromptConstruction(t *testing.T) {
	phase := Phase{Number: 1, Name: "Foundation", Tasks: []Task{{Text: "Initialize module"}, {Text: "Set up structure"}}}

	if got := ApplyPrompt("initial-scaffold", phase); !strings.Contains(got, "litespec-apply skill") || !strings.Contains(got, "phase 1 of change initial-scaffold") || !strings.Contains(got, "Initialize module") {
		t.Fatalf("ApplyPrompt() = %q", got)
	}
	if got := ReviewPrompt("initial-scaffold"); !strings.Contains(got, "litespec-review skill") {
		t.Fatalf("ReviewPrompt() = %q", got)
	}
	if got := FixPrompt("issues found"); !strings.Contains(got, "The review found these issues: issues found. Fix them.") {
		t.Fatalf("FixPrompt() = %q", got)
	}
	if got := FinalReviewPrompt("initial-scaffold"); !strings.Contains(got, "pre-archive mode") {
		t.Fatalf("FinalReviewPrompt() = %q", got)
	}
}

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
