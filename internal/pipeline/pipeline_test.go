package pipeline

import (
	"context"
	"fmt"
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

func (f *fakeAgent) Name() string                                 { return "fake" }
func (f *fakeAgent) CheckPrerequisites(ctx context.Context) error { return nil }
func (f *fakeAgent) Cleanup(ctx context.Context) error            { return nil }
func (f *fakeAgent) AsRPC() agent.RPCAgent                        { return nil }
func (f *fakeAgent) Events() <-chan agent.Event                   { return nil }

func (f *fakeAgent) ExecuteStep(ctx context.Context, step agent.Step, prompt, model string) (*agent.Result, error) {
	_ = model
	f.calls = append(f.calls, string(step)+":"+prompt)
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
	calls       []string
	cleanErr    error
	branchExist bool
}

func (f *fakeGit) IsClean(ctx context.Context) error {
	f.calls = append(f.calls, "is-clean")
	return f.cleanErr
}

func (f *fakeGit) BranchExists(ctx context.Context, branch string) (bool, error) {
	f.calls = append(f.calls, "branch-exists:"+branch)
	return f.branchExist, nil
}

func (f *fakeGit) CreateBranch(ctx context.Context, branch string) error {
	f.calls = append(f.calls, "branch:"+branch)
	return nil
}

func (f *fakeGit) Checkout(ctx context.Context, branch string) error {
	f.calls = append(f.calls, "checkout:"+branch)
	return nil
}

func (f *fakeGit) Add(ctx context.Context, paths ...string) error {
	f.calls = append(f.calls, "add:"+strings.Join(paths, ","))
	return nil
}

func (f *fakeGit) AddAll(ctx context.Context) error {
	f.calls = append(f.calls, "add-all")
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

	fa := &fakeAgent{results: []agent.Result{
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

	p := &Pipeline{ChangePath: changeDir, RepoDir: changeDir, Agent: fa, Git: git, PR: pr, MaxAttempts: 3}
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
		"is-clean",
		"branch-exists:change/initial-scaffold",
		"branch:change/initial-scaffold",
		"add-all",
		"commit:chore: add litespec artifacts for initial-scaffold|Planning artifacts for initial-scaffold\n\nPhase 1: Foundation\nPhase 2: Adapter",
		"add-all",
		"commit:feat(initial-scaffold): complete phase 1 - Foundation|Phase 1: Foundation\n- [x] Task one",
		"add-all",
		"commit:feat(initial-scaffold): complete phase 2 - Adapter|Phase 2: Adapter\n- [x] Task two",
		"push:origin|change/initial-scaffold|true",
	}
	if !reflect.DeepEqual(git.calls, wantGitCalls) {
		t.Fatalf("git.calls = %#v, want %#v", git.calls, wantGitCalls)
	}

	if got, want := len(fa.calls), 7; got != want {
		t.Fatalf("len(agent.calls) = %d, want %d", got, want)
	}
	if !strings.Contains(fa.calls[0], "litespec-apply skill") || !strings.Contains(fa.calls[1], "litespec-review skill") {
		t.Fatalf("agent prompts not constructed as expected: %#v", fa.calls[:2])
	}
}

func TestPipelineDirtyWorktreeAborts(t *testing.T) {
	tmp := t.TempDir()
	changeDir := filepath.Join(tmp, "initial-scaffold")
	if err := writeFile(filepath.Join(changeDir, "tasks.md"), "# Tasks\n\n## Phase 1: Foundation\n- [ ] Task one\n"); err != nil {
		t.Fatalf("write tasks: %v", err)
	}

	git := &fakeGit{cleanErr: fmt.Errorf("dirty worktree")}
	p := &Pipeline{ChangePath: changeDir, RepoDir: changeDir, Agent: &fakeAgent{}, Git: git, MaxAttempts: 3}
	_, err := p.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want error for dirty worktree")
	}
	if !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("Run() error = %q, want uncommitted changes message", err.Error())
	}
}

func TestPipelineBranchExistsDeclineAborts(t *testing.T) {
	tmp := t.TempDir()
	changeDir := filepath.Join(tmp, "initial-scaffold")
	if err := writeFile(filepath.Join(changeDir, "tasks.md"), "# Tasks\n\n## Phase 1: Foundation\n- [ ] Task one\n"); err != nil {
		t.Fatalf("write tasks: %v", err)
	}

	git := &fakeGit{branchExist: true}
	p := &Pipeline{
		ChangePath:  changeDir,
		RepoDir:     changeDir,
		Agent:       &fakeAgent{},
		Git:         git,
		MaxAttempts: 3,
		Confirm:     func(prompt string) bool { return false },
	}
	_, err := p.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want error when declining branch reuse")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("Run() error = %q, want branch exists message", err.Error())
	}
}

func TestPipelineBranchExistsAcceptReuses(t *testing.T) {
	tmp := t.TempDir()
	changeDir := filepath.Join(tmp, "initial-scaffold")
	if err := writeFile(filepath.Join(changeDir, "tasks.md"), "# Tasks\n\n## Phase 1: Foundation\n- [ ] Task one\n"); err != nil {
		t.Fatalf("write tasks: %v", err)
	}
	if err := writeFile(filepath.Join(changeDir, "proposal.md"), "proposal\n"); err != nil {
		t.Fatalf("write proposal: %v", err)
	}

	git := &fakeGit{branchExist: true}
	pr := &fakePR{}
	fa := &fakeAgent{results: []agent.Result{
		{Stdout: "applied"},
		{Stdout: "no issues found"},
		{Stdout: "final review: no issues found"},
	}}
	p := &Pipeline{
		ChangePath:  changeDir,
		RepoDir:     changeDir,
		Agent:       fa,
		Git:         git,
		PR:          pr,
		MaxAttempts: 3,
		Confirm:     func(prompt string) bool { return true },
	}
	report, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	for _, call := range git.calls {
		if strings.HasPrefix(call, "branch:") {
			t.Fatalf("should not create branch when reusing, got call: %s", call)
		}
	}
	var checkedOut bool
	for _, call := range git.calls {
		if call == "checkout:change/initial-scaffold" {
			checkedOut = true
		}
	}
	if !checkedOut {
		t.Fatal("expected checkout:change/initial-scaffold call when reusing branch")
	}
	if !report.FinalReviewPassed {
		t.Fatal("FinalReviewPassed = false, want true")
	}
}

func TestPipelineMaxRetriesEmitsTaunt(t *testing.T) {
	tmp := t.TempDir()
	changeDir := filepath.Join(tmp, "taunt-test")
	if err := writeFile(filepath.Join(changeDir, "tasks.md"), "# Tasks\n\n## Phase 1: Foundation\n- [ ] Task one\n"); err != nil {
		t.Fatalf("write tasks: %v", err)
	}
	if err := writeFile(filepath.Join(changeDir, "proposal.md"), "proposal\n"); err != nil {
		t.Fatalf("write proposal: %v", err)
	}

	fa := &fakeAgent{results: []agent.Result{
		{Stdout: "applied"},
		{Stdout: "issues found: fix this"},
		{Stdout: "applied fix"},
		{Stdout: "issues found: still broken"},
		{Stdout: "applied fix again"},
		{Stdout: "issues found: persistent failure"},
	}}
	git := &fakeGit{}
	pr := &fakePR{}

	var events []Event
	p := &Pipeline{
		ChangePath:  changeDir,
		RepoDir:     changeDir,
		Agent:       fa,
		Git:         git,
		PR:          pr,
		MaxAttempts: 3,
		OnEvent: func(msg Event) {
			events = append(events, msg)
		},
	}
	report, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if report.Phases[0].Passed {
		t.Fatal("phase 1 should not pass after max retries with persistent failures")
	}
	if report.Phases[0].Attempts != 3 {
		t.Fatalf("Attempts = %d, want 3", report.Phases[0].Attempts)
	}

	var tauntFound bool
	for _, e := range events {
		if _, ok := e.(TauntEvent); ok {
			tauntFound = true
			break
		}
	}
	if !tauntFound {
		t.Fatal("no TauntEvent emitted after max retries exhausted")
	}

	// Pipeline should NOT push or create PR when a phase failed.
	for _, call := range git.calls {
		if strings.HasPrefix(call, "push:") {
			t.Fatal("pipeline should not push after phase failure")
		}
	}
	if pr.call != "" {
		t.Fatal("pipeline should not create PR after phase failure")
	}
	if report.FinalReviewPassed {
		t.Fatal("FinalReviewPassed should be false when phases failed")
	}
}

func TestPipelineFinalReviewRetriesOnFailure(t *testing.T) {
	tmp := t.TempDir()
	changeDir := filepath.Join(tmp, "final-review-test")
	if err := writeFile(filepath.Join(changeDir, "tasks.md"), "# Tasks\n\n## Phase 1: Foundation\n- [ ] Task one\n"); err != nil {
		t.Fatalf("write tasks: %v", err)
	}
	if err := writeFile(filepath.Join(changeDir, "proposal.md"), "proposal\n"); err != nil {
		t.Fatalf("write proposal: %v", err)
	}

	fa := &fakeAgent{results: []agent.Result{
		{Stdout: "applied"},
		{Stdout: "no issues found"},
		{Stdout: "issues found: final problems"},
		{Stdout: "applied fix"},
		{Stdout: "no issues found"},
	}}
	git := &fakeGit{}
	pr := &fakePR{}

	p := &Pipeline{ChangePath: changeDir, RepoDir: changeDir, Agent: fa, Git: git, PR: pr, MaxAttempts: 3}
	report, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !report.FinalReviewPassed {
		t.Fatal("FinalReviewPassed = false, want true after retry")
	}
	if got, want := len(fa.calls), 5; got != want {
		t.Fatalf("len(agent.calls) = %d, want %d (apply, review, final-review, fix, final-review)", got, want)
	}
}

func TestPipelineEmitsSummaryEvent(t *testing.T) {
	tmp := t.TempDir()
	changeDir := filepath.Join(tmp, "summary-test")
	if err := writeFile(filepath.Join(changeDir, "tasks.md"), "# Tasks\n\n## Phase 1: Foundation\n- [ ] Task one\n"); err != nil {
		t.Fatalf("write tasks: %v", err)
	}
	if err := writeFile(filepath.Join(changeDir, "proposal.md"), "proposal\n"); err != nil {
		t.Fatalf("write proposal: %v", err)
	}

	fa := &fakeAgent{results: []agent.Result{
		{Stdout: "applied"},
		{Stdout: "no issues found"},
		{Stdout: "final review: no issues found"},
	}}
	git := &fakeGit{}
	pr := &fakePR{}

	var events []Event
	p := &Pipeline{
		ChangePath:  changeDir,
		RepoDir:     changeDir,
		Agent:       fa,
		Git:         git,
		PR:          pr,
		MaxAttempts: 3,
		OnEvent: func(msg Event) {
			events = append(events, msg)
		},
	}
	_, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var summary *SummaryEvent
	for _, e := range events {
		if s, ok := e.(SummaryEvent); ok {
			summary = &s
			break
		}
	}
	if summary == nil {
		t.Fatal("no SummaryEvent emitted")
	}
	if summary.PhasesCompleted != 1 {
		t.Fatalf("PhasesCompleted = %d, want 1", summary.PhasesCompleted)
	}
	if summary.PhasesFailed != 0 {
		t.Fatalf("PhasesFailed = %d, want 0", summary.PhasesFailed)
	}
	if summary.Branch != "change/summary-test" {
		t.Fatalf("Branch = %q, want change/summary-test", summary.Branch)
	}
	if summary.Duration == "" {
		t.Fatal("Duration is empty")
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
		{name: "mixed negative wins", output: "no issues found but failed", exitCode: 0, want: false},
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

	if got := ApplyPrompt("initial-scaffold", phase); !strings.Contains(got, "litespec-apply skill") || !strings.Contains(got, "specs/changes/initial-scaffold/proposal.md") || !strings.Contains(got, "Initialize module") {
		t.Fatalf("ApplyPrompt() = %q", got)
	}
	if got := ReviewPrompt("initial-scaffold"); !strings.Contains(got, "litespec-review skill") || !strings.Contains(got, "specs/changes/initial-scaffold/proposal.md") {
		t.Fatalf("ReviewPrompt() = %q", got)
	}
	if got := FixPrompt("issues found"); !strings.Contains(got, "The review found these issues: issues found. Fix them.") {
		t.Fatalf("FixPrompt() = %q", got)
	}
	if got := FinalReviewPrompt("initial-scaffold"); !strings.Contains(got, "litespec-review skill") || !strings.Contains(got, "pre-archive mode") {
		t.Fatalf("FinalReviewPrompt() = %q", got)
	}
}

func TestPipelineContextCancellation(t *testing.T) {
	tmp := t.TempDir()
	changeDir := filepath.Join(tmp, "cancel-test")
	if err := writeFile(filepath.Join(changeDir, "tasks.md"), "# Tasks\n\n## Phase 1: Foundation\n- [ ] Task one\n"); err != nil {
		t.Fatalf("write tasks: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	fa := &fakeAgent{}
	git := &fakeGit{}
	p := &Pipeline{ChangePath: changeDir, RepoDir: changeDir, Agent: fa, Git: git, MaxAttempts: 3}
	_, err := p.Run(ctx)
	if err == nil {
		t.Fatal("Run() error = nil, want context canceled error")
	}
	if !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("Run() error = %q, want context canceled", err.Error())
	}
}

func TestPipelinePhaseFailureStopsSubsequentPhases(t *testing.T) {
	tmp := t.TempDir()
	changeDir := filepath.Join(tmp, "stop-test")
	if err := writeFile(filepath.Join(changeDir, "tasks.md"), "# Tasks\n\n## Phase 1: Foundation\n- [ ] Task one\n\n## Phase 2: Adapter\n- [ ] Task two\n"); err != nil {
		t.Fatalf("write tasks: %v", err)
	}
	if err := writeFile(filepath.Join(changeDir, "proposal.md"), "proposal\n"); err != nil {
		t.Fatalf("write proposal: %v", err)
	}

	fa := &fakeAgent{results: []agent.Result{
		{Stdout: "applied"},
		{Stdout: "issues found: broken"},
		{Stdout: "applied fix"},
		{Stdout: "issues found: still broken"},
		{Stdout: "applied fix 2"},
		{Stdout: "issues found: persistent"},
	}}
	git := &fakeGit{}
	pr := &fakePR{}

	p := &Pipeline{ChangePath: changeDir, RepoDir: changeDir, Agent: fa, Git: git, PR: pr, MaxAttempts: 3}
	report, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	// Only phase 1 should be attempted; phase 2 should not run.
	if got := len(report.Phases); got != 1 {
		t.Fatalf("len(Phases) = %d, want 1 (phase 2 should not execute after phase 1 failure)", got)
	}
	if report.Phases[0].Passed {
		t.Fatal("phase 1 should have failed")
	}
}

func TestPipelineNoCommitOnFailedPhase(t *testing.T) {
	tmp := t.TempDir()
	changeDir := filepath.Join(tmp, "no-commit-test")
	if err := writeFile(filepath.Join(changeDir, "tasks.md"), "# Tasks\n\n## Phase 1: Foundation\n- [ ] Task one\n"); err != nil {
		t.Fatalf("write tasks: %v", err)
	}
	if err := writeFile(filepath.Join(changeDir, "proposal.md"), "proposal\n"); err != nil {
		t.Fatalf("write proposal: %v", err)
	}

	fa := &fakeAgent{results: []agent.Result{
		{Stdout: "applied"},
		{Stdout: "issues found: broken"},
		{Stdout: "applied fix"},
		{Stdout: "issues found: still broken"},
		{Stdout: "applied fix 2"},
		{Stdout: "issues found: persistent"},
	}}
	git := &fakeGit{}
	pr := &fakePR{}

	p := &Pipeline{ChangePath: changeDir, RepoDir: changeDir, Agent: fa, Git: git, PR: pr, MaxAttempts: 3}
	_, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	// The only commit should be the artifacts commit; no phase commit.
	var commitCount int
	for _, call := range git.calls {
		if strings.HasPrefix(call, "commit:feat(") {
			commitCount++
		}
	}
	if commitCount != 0 {
		t.Fatalf("expected 0 phase commits on failure, got %d", commitCount)
	}
}

func TestReviewPassedStructuredMarkers(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{name: "pass marker", output: "some review text\npass", want: true},
		{name: "PASS marker", output: "review\nPASS", want: true},
		{name: "lgtm marker", output: "looks fine\nlgtm", want: true},
		{name: "approved marker", output: "all good\napproved", want: true},
		{name: "fail marker", output: "some review text\nfail", want: false},
		{name: "rejected marker", output: "problems found\nrejected", want: false},
		{name: "blocked marker", output: "cannot merge\nblocked", want: false},
		{name: "marker with trailing whitespace", output: "review\n  pass  \n", want: true},
		{name: "marker overrides heuristic", output: "issues found: something\npass", want: true},
		{name: "fail marker overrides ambiguous", output: "looks fine\nfail", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ReviewPassed(tt.output, 0); got != tt.want {
				t.Fatalf("ReviewPassed(%q, 0) = %v, want %v", tt.output, got, tt.want)
			}
		})
	}
}

func TestReviewPassedNegativePhrasesCheckedFirst(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{name: "failed overrides no issues found", output: "no issues found but build failed", want: false},
		{name: "blocking overrides no issues found", output: "no issues found but blocking concern", want: false},
		{name: "needs work overrides no issues found", output: "no issues found but needs work", want: false},
		{name: "failure overrides no issues found", output: "no issues found, failure detected", want: false},
		{name: "issues found without no prefix is failure", output: "issues found: broken things", want: false},
		{name: "no issues found alone is pass", output: "no issues found", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ReviewPassed(tt.output, 0); got != tt.want {
				t.Fatalf("ReviewPassed(%q, 0) = %v, want %v", tt.output, got, tt.want)
			}
		})
	}
}

func TestPipelineFinalReviewFailureBlocksPushAndPR(t *testing.T) {
	tmp := t.TempDir()
	changeDir := filepath.Join(tmp, "final-block-test")
	if err := writeFile(filepath.Join(changeDir, "tasks.md"), "# Tasks\n\n## Phase 1: Foundation\n- [ ] Task one\n"); err != nil {
		t.Fatalf("write tasks: %v", err)
	}
	if err := writeFile(filepath.Join(changeDir, "proposal.md"), "proposal\n"); err != nil {
		t.Fatalf("write proposal: %v", err)
	}

	fa := &fakeAgent{results: []agent.Result{
		{Stdout: "applied"},
		{Stdout: "no issues found"},
		{Stdout: "issues found: final problems"},
		{Stdout: "applied fix"},
		{Stdout: "issues found: still broken"},
		{Stdout: "applied fix again"},
		{Stdout: "issues found: persistent failure"},
	}}
	git := &fakeGit{}
	pr := &fakePR{}

	p := &Pipeline{ChangePath: changeDir, RepoDir: changeDir, Agent: fa, Git: git, PR: pr, MaxAttempts: 3}
	report, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if report.FinalReviewPassed {
		t.Fatal("FinalReviewPassed = true, want false after all final review attempts fail")
	}
	for _, call := range git.calls {
		if strings.HasPrefix(call, "push:") {
			t.Fatal("pipeline should not push when final review fails")
		}
	}
	if pr.call != "" {
		t.Fatal("pipeline should not create PR when final review fails")
	}
}

func TestPipelineApplyNonZeroExitCodeTreatedAsFailure(t *testing.T) {
	tmp := t.TempDir()
	changeDir := filepath.Join(tmp, "exit-code-test")
	if err := writeFile(filepath.Join(changeDir, "tasks.md"), "# Tasks\n\n## Phase 1: Foundation\n- [ ] Task one\n"); err != nil {
		t.Fatalf("write tasks: %v", err)
	}
	if err := writeFile(filepath.Join(changeDir, "proposal.md"), "proposal\n"); err != nil {
		t.Fatalf("write proposal: %v", err)
	}

	fa := &fakeAgent{results: []agent.Result{
		{Stdout: "applied with errors", ExitCode: 1},
		{Stdout: "applied fix"},
		{Stdout: "no issues found"},
		{Stdout: "final review: no issues found"},
	}}
	git := &fakeGit{}
	pr := &fakePR{}

	p := &Pipeline{ChangePath: changeDir, RepoDir: changeDir, Agent: fa, Git: git, PR: pr, MaxAttempts: 3}
	report, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !report.Phases[0].Passed {
		t.Fatal("phase should pass after fix despite initial non-zero exit code")
	}
	if got, want := report.Phases[0].Attempts, 2; got != want {
		t.Fatalf("Attempts = %d, want %d (apply fail + fix+review pass)", got, want)
	}
	if !report.FinalReviewPassed {
		t.Fatal("FinalReviewPassed = false, want true")
	}
}

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
