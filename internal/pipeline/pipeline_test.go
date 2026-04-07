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
	"time"

	"github.com/bermudi/nelsonctl/internal/agent"
	"github.com/bermudi/nelsonctl/internal/config"
	ctrl "github.com/bermudi/nelsonctl/internal/controller"
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
	_ = ctx
	f.calls = append(f.calls, fmt.Sprintf("%s|%s|%s", step, model, prompt))
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
	calls         []string
	cleanErr      error
	currentBranch string
	branchExist   bool
	changedFiles  []string
	diff          string
	hasTracked    bool
}

func (f *fakeGit) IsClean(ctx context.Context) error {
	f.calls = append(f.calls, "is-clean")
	return f.cleanErr
}

func (f *fakeGit) CurrentBranch(ctx context.Context) (string, error) {
	f.calls = append(f.calls, "current-branch")
	if f.currentBranch == "" {
		return "main", nil
	}
	return f.currentBranch, nil
}

func (f *fakeGit) Diff(ctx context.Context) (string, error) {
	f.calls = append(f.calls, "diff")
	return f.diff, nil
}

func (f *fakeGit) HasTrackedChanges(ctx context.Context) (bool, error) {
	f.calls = append(f.calls, "has-tracked")
	return f.hasTracked, nil
}

func (f *fakeGit) ChangedFiles(ctx context.Context) ([]string, error) {
	f.calls = append(f.calls, "changed-files")
	return append([]string(nil), f.changedFiles...), nil
}

func (f *fakeGit) BranchExists(ctx context.Context, branch string) (bool, error) {
	f.calls = append(f.calls, "branch-exists:"+branch)
	return f.branchExist, nil
}

func (f *fakeGit) CreateBranch(ctx context.Context, branch string) error {
	f.calls = append(f.calls, "branch:"+branch)
	f.currentBranch = branch
	return nil
}

func (f *fakeGit) Checkout(ctx context.Context, branch string) error {
	f.calls = append(f.calls, "checkout:"+branch)
	f.currentBranch = branch
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

type fakeController struct {
	phaseRuns []ctrl.PhaseRequest
	finalRuns []ctrl.FinalReviewRequest
	phaseFn   func(ctx context.Context, request ctrl.PhaseRequest, dispatcher ctrl.Dispatcher) (*ctrl.Result, error)
	finalFn   func(ctx context.Context, request ctrl.FinalReviewRequest, dispatcher ctrl.Dispatcher) (*ctrl.Result, error)
}

func (f *fakeController) RunPhase(ctx context.Context, request ctrl.PhaseRequest, dispatcher ctrl.Dispatcher) (*ctrl.Result, error) {
	f.phaseRuns = append(f.phaseRuns, request)
	if f.phaseFn != nil {
		return f.phaseFn(ctx, request, dispatcher)
	}
	return &ctrl.Result{Summary: "approved"}, nil
}

func (f *fakeController) RunFinalReview(ctx context.Context, request ctrl.FinalReviewRequest, dispatcher ctrl.Dispatcher) (*ctrl.Result, error) {
	f.finalRuns = append(f.finalRuns, request)
	if f.finalFn != nil {
		return f.finalFn(ctx, request, dispatcher)
	}
	return &ctrl.Result{Summary: "approved"}, nil
}

func (f *fakeController) Continue(ctx context.Context, messages []ctrl.Message, dispatcher ctrl.Dispatcher) (*ctrl.Result, error) {
	return nil, fmt.Errorf("unexpected Continue call")
}

func TestPipelineRunUsesControllerAndScopedPhaseCommit(t *testing.T) {
	tmp := t.TempDir()
	changeDir := filepath.Join(tmp, "specs", "changes", "initial-scaffold")
	mustWriteFile(t, filepath.Join(changeDir, "tasks.md"), "# Tasks\n\n## Phase 1: Foundation\n- [ ] Task one\n")
	mustWriteFile(t, filepath.Join(changeDir, "proposal.md"), "proposal\n")

	fa := &fakeAgent{results: []agent.Result{{Stdout: "apply ok"}, {Stdout: "review ok"}}}
	git := &fakeGit{changedFiles: []string{"internal/pipeline/pipeline.go"}, diff: "diff --git a/file b/file"}
	pr := &fakePR{note: "created"}
	controller := &fakeController{phaseFn: func(ctx context.Context, request ctrl.PhaseRequest, dispatcher ctrl.Dispatcher) (*ctrl.Result, error) {
		if request.ReviewFailOn != config.FailOnWarning {
			t.Fatalf("ReviewFailOn = %q", request.ReviewFailOn)
		}
		if len(request.Phase.Tasks) != 1 || request.Phase.Tasks[0] != "Task one" {
			t.Fatalf("phase tasks = %#v", request.Phase.Tasks)
		}
		if _, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "1", Name: ctrl.ToolSubmitPrompt, Arguments: []byte(`{"prompt":"apply this phase"}`)}); err != nil {
			return nil, err
		}
		if _, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "2", Name: ctrl.ToolRunReview, Arguments: []byte(`{}`)}); err != nil {
			return nil, err
		}
		approve, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "3", Name: ctrl.ToolApprove, Arguments: []byte(`{"summary":"phase passed"}`)})
		if err != nil {
			return nil, err
		}
		return &ctrl.Result{Summary: approve.Summary}, nil
	}, finalFn: func(ctx context.Context, request ctrl.FinalReviewRequest, dispatcher ctrl.Dispatcher) (*ctrl.Result, error) {
		if _, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "4", Name: ctrl.ToolRunReview, Arguments: []byte(`{}`)}); err != nil {
			return nil, err
		}
		approve, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "5", Name: ctrl.ToolApprove, Arguments: []byte(`{"summary":"final passed"}`)})
		if err != nil {
			return nil, err
		}
		return &ctrl.Result{Summary: approve.Summary}, nil
	}}

	p := &Pipeline{ChangePath: changeDir, RepoDir: tmp, Agent: fa, Controller: controller, Git: git, PR: pr, MaxAttempts: 3, Config: config.Config{Review: config.ReviewConfig{FailOn: config.FailOnWarning}, Steps: config.DefaultConfig().Steps}, processExists: func(pid int) bool { return false }, now: func() time.Time { return time.Unix(1, 0) }}
	report, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !report.FinalReviewPassed || len(report.Phases) != 1 || !report.Phases[0].Passed {
		t.Fatalf("unexpected report: %#v", report)
	}
	if len(controller.phaseRuns) != 1 || len(controller.finalRuns) != 1 {
		t.Fatalf("controller runs phase=%d final=%d", len(controller.phaseRuns), len(controller.finalRuns))
	}
	wantGitCalls := []string{
		"current-branch",
		"branch-exists:change/initial-scaffold",
		"is-clean",
		"branch:change/initial-scaffold",
		"has-tracked",
		"add-all",
		"commit:chore: add litespec artifacts for initial-scaffold|Planning artifacts for initial-scaffold\n\nPhase 1: Foundation",
		"changed-files",
		"add:internal/pipeline/pipeline.go",
		"commit:feat(initial-scaffold): complete phase 1 - Foundation|Phase 1: Foundation\n- [x] Task one",
		"push:origin|change/initial-scaffold|true",
	}
	if !reflect.DeepEqual(git.calls, wantGitCalls) {
		t.Fatalf("git.calls = %#v, want %#v", git.calls, wantGitCalls)
	}
	if got, want := len(fa.calls), 3; got != want {
		t.Fatalf("len(agent.calls) = %d, want %d", got, want)
	}
	if !strings.Contains(fa.calls[0], "apply|minimax/minimax-m2.7|apply this phase") {
		t.Fatalf("unexpected apply call: %q", fa.calls[0])
	}
	if !strings.Contains(fa.calls[1], "review|moonshotai/kimi-k2.5|Use your litespec-review skill") {
		t.Fatalf("unexpected review call: %q", fa.calls[1])
	}
	if !strings.Contains(fa.calls[2], "final_review|moonshotai/kimi-k2.5|Use your litespec-review skill in pre-archive mode") {
		t.Fatalf("unexpected final review call: %q", fa.calls[2])
	}
}

func TestPipelineResumeCreatesRecoveryCommitAndStartsAtFirstUncheckedPhase(t *testing.T) {
	tmp := t.TempDir()
	changeDir := filepath.Join(tmp, "specs", "changes", "resume-test")
	mustWriteFile(t, filepath.Join(changeDir, "tasks.md"), "# Tasks\n\n## Phase 1: One\n- [x] done\n\n## Phase 2: Two\n- [ ] next\n")
	mustWriteFile(t, filepath.Join(changeDir, "proposal.md"), "proposal\n")

	fa := &fakeAgent{results: []agent.Result{{Stdout: "apply ok"}, {Stdout: "review ok"}, {Stdout: "final ok"}}}
	git := &fakeGit{currentBranch: "change/resume-test", branchExist: true, changedFiles: []string{"file.go"}, hasTracked: true}
	controller := &fakeController{phaseFn: func(ctx context.Context, request ctrl.PhaseRequest, dispatcher ctrl.Dispatcher) (*ctrl.Result, error) {
		if request.Phase.Number != 2 {
			t.Fatalf("phase number = %d, want 2", request.Phase.Number)
		}
		if _, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "1", Name: ctrl.ToolSubmitPrompt, Arguments: []byte(`{"prompt":"resume phase"}`)}); err != nil {
			return nil, err
		}
		if _, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "2", Name: ctrl.ToolRunReview, Arguments: []byte(`{}`)}); err != nil {
			return nil, err
		}
		if _, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "3", Name: ctrl.ToolApprove, Arguments: []byte(`{"summary":"ok"}`)}); err != nil {
			return nil, err
		}
		return &ctrl.Result{Summary: "ok"}, nil
	}, finalFn: func(ctx context.Context, request ctrl.FinalReviewRequest, dispatcher ctrl.Dispatcher) (*ctrl.Result, error) {
		if _, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "4", Name: ctrl.ToolRunReview, Arguments: []byte(`{}`)}); err != nil {
			return nil, err
		}
		if _, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "5", Name: ctrl.ToolApprove, Arguments: []byte(`{"summary":"final"}`)}); err != nil {
			return nil, err
		}
		return &ctrl.Result{Summary: "final"}, nil
	}}

	p := &Pipeline{ChangePath: changeDir, RepoDir: tmp, Agent: fa, Controller: controller, Git: git, PR: &fakePR{}, MaxAttempts: 3, Config: config.Config{Review: config.ReviewConfig{FailOn: config.FailOnCritical}, Steps: config.DefaultConfig().Steps}, processExists: func(pid int) bool { return false }, now: time.Now}
	report, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !report.Resumed {
		t.Fatal("Resumed = false, want true")
	}
	for _, want := range []string{"has-tracked", "add:file.go", "commit:chore: recovery commit|Preserve resumable tracked work before continuing the controller pipeline."} {
		if !containsCall(git.calls, want) {
			t.Fatalf("git.calls missing %q in %#v", want, git.calls)
		}
	}
	if containsCallPrefix(git.calls, "add-all") {
		t.Fatalf("unexpected artifacts commit on resume: %#v", git.calls)
	}
}

func TestPipelineRefusesDirtyUnrelatedBranch(t *testing.T) {
	tmp := t.TempDir()
	changeDir := filepath.Join(tmp, "specs", "changes", "dirty-test")
	mustWriteFile(t, filepath.Join(changeDir, "tasks.md"), "# Tasks\n\n## Phase 1: One\n- [ ] todo\n")

	p := &Pipeline{ChangePath: changeDir, RepoDir: tmp, Agent: &fakeAgent{}, Controller: &fakeController{}, Git: &fakeGit{currentBranch: "main", branchExist: true, cleanErr: fmt.Errorf("dirty")}, Config: config.Config{Steps: config.DefaultConfig().Steps}, processExists: func(pid int) bool { return false }, now: time.Now}
	_, err := p.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unrelated branch") {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestPipelineLockHandling(t *testing.T) {
	tmp := t.TempDir()
	p := &Pipeline{processExists: func(pid int) bool { return pid == 123 }, now: func() time.Time { return time.Unix(2, 0) }}
	lockPath := filepath.Join(tmp, ".nelsonctl.lock")
	mustWriteFile(t, lockPath, `{"pid":123,"timestamp":"2026-01-01T00:00:00Z"}`)
	if _, err := p.acquireLock(lockPath); err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("acquireLock() error = %v", err)
	}
	mustWriteFile(t, lockPath, `{"pid":999,"timestamp":"2026-01-01T00:00:00Z"}`)
	unlock, err := p.acquireLock(lockPath)
	if err != nil {
		t.Fatalf("acquireLock() stale error = %v", err)
	}
	if err := unlock(); err != nil {
		t.Fatalf("unlock() error = %v", err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("lock file should be removed, stat err = %v", err)
	}
}

func TestMechanicalReviewPrompt(t *testing.T) {
	if got := MechanicalReviewPrompt("demo", false); !strings.Contains(got, "Use your litespec-review skill to review the implementation of change demo") {
		t.Fatalf("MechanicalReviewPrompt(false) = %q", got)
	}
	if got := MechanicalReviewPrompt("demo", true); !strings.Contains(got, "pre-archive mode") {
		t.Fatalf("MechanicalReviewPrompt(true) = %q", got)
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

func containsCall(calls []string, want string) bool {
	for _, call := range calls {
		if call == want {
			return true
		}
	}
	return false
}

func containsCallPrefix(calls []string, prefix string) bool {
	for _, call := range calls {
		if strings.HasPrefix(call, prefix) {
			return true
		}
	}
	return false
}
