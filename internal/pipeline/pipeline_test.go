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
	rpc     agent.RPCAgent
}

func (f *fakeAgent) Name() string                                 { return "fake" }
func (f *fakeAgent) CheckPrerequisites(ctx context.Context) error { return nil }
func (f *fakeAgent) Cleanup(ctx context.Context) error            { return nil }
func (f *fakeAgent) AsRPC() agent.RPCAgent                        { return f.rpc }
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
	stagedFiles   []string
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

func (f *fakeGit) StagedFiles(ctx context.Context) ([]string, error) {
	f.calls = append(f.calls, "staged-files")
	if len(f.stagedFiles) > 0 {
		return append([]string(nil), f.stagedFiles...), nil
	}
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
	url  string
}

func (f *fakePR) Create(ctx context.Context, repoDir, title, bodyFile string) (string, string, error) {
	f.call = repoDir + "|" + title + "|" + bodyFile
	return f.note, f.url, nil
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

	fa := &fakeAgent{results: []agent.Result{{Stdout: "apply ok"}, {Stdout: "review ok"}, {Stdout: "commit ok"}, {Stdout: "final review ok"}}}
	git := &fakeGit{changedFiles: []string{"internal/pipeline/pipeline.go"}, stagedFiles: []string{"internal/pipeline/pipeline.go"}, diff: "diff --git a/file b/file"}
	pr := &fakePR{note: "created", url: "https://example.test/pr/1"}
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
		if _, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "3", Name: ctrl.ToolCommit, Arguments: []byte(`{"message":"feat(test): phase 1"}`)}); err != nil {
			return nil, err
		}
		approve, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "4", Name: ctrl.ToolApprove, Arguments: []byte(`{"summary":"phase passed"}`)})
		if err != nil {
			return nil, err
		}
		return &ctrl.Result{Summary: approve.Summary}, nil
	}, finalFn: func(ctx context.Context, request ctrl.FinalReviewRequest, dispatcher ctrl.Dispatcher) (*ctrl.Result, error) {
		if _, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "5", Name: ctrl.ToolRunReview, Arguments: []byte(`{}`)}); err != nil {
			return nil, err
		}
		approve, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "6", Name: ctrl.ToolApprove, Arguments: []byte(`{"summary":"final passed"}`)})
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
		"push:origin|change/initial-scaffold|true",
	}
	if !reflect.DeepEqual(git.calls, wantGitCalls) {
		t.Fatalf("git.calls = %#v, want %#v", git.calls, wantGitCalls)
	}
	// Agent called 4 times: apply, review, commit, final_review
	if got, want := len(fa.calls), 4; got != want {
		t.Fatalf("len(agent.calls) = %d, want %d", got, want)
	}
	if !strings.Contains(fa.calls[0], "apply|minimax/minimax-m2.7|apply this phase") {
		t.Fatalf("unexpected apply call: %q", fa.calls[0])
	}
	if !strings.Contains(fa.calls[1], "review|moonshotai/kimi-k2.5|Use your litespec-review skill") {
		t.Fatalf("unexpected review call: %q", fa.calls[1])
	}
	if !strings.Contains(fa.calls[2], "commit|minimax/minimax-m2.7|Stage all your changes") {
		t.Fatalf("unexpected commit call: %q", fa.calls[2])
	}
	if !strings.Contains(fa.calls[3], "final_review|moonshotai/kimi-k2.5|Use your litespec-review skill in pre-archive mode") {
		t.Fatalf("unexpected final review call: %q", fa.calls[3])
	}
}

func TestPipelineResumeCreatesRecoveryCommitAndStartsAtFirstUncheckedPhase(t *testing.T) {
	tmp := t.TempDir()
	changeDir := filepath.Join(tmp, "specs", "changes", "resume-test")
	mustWriteFile(t, filepath.Join(changeDir, "tasks.md"), "# Tasks\n\n## Phase 1: One\n- [x] done\n\n## Phase 2: Two\n- [ ] next\n")
	mustWriteFile(t, filepath.Join(changeDir, "proposal.md"), "proposal\n")

	fa := &fakeAgent{results: []agent.Result{{Stdout: "apply ok"}, {Stdout: "review ok"}, {Stdout: "commit ok"}, {Stdout: "final ok"}}}
	git := &fakeGit{currentBranch: "change/resume-test", branchExist: true, changedFiles: []string{"file.go"}, stagedFiles: []string{"file.go"}, hasTracked: true}
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
		if _, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "3", Name: ctrl.ToolCommit, Arguments: []byte(`{"message":"feat(resume-test): phase 2"}`)}); err != nil {
			return nil, err
		}
		if _, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "4", Name: ctrl.ToolApprove, Arguments: []byte(`{"summary":"ok"}`)}); err != nil {
			return nil, err
		}
		return &ctrl.Result{Summary: "ok"}, nil
	}, finalFn: func(ctx context.Context, request ctrl.FinalReviewRequest, dispatcher ctrl.Dispatcher) (*ctrl.Result, error) {
		if _, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "5", Name: ctrl.ToolRunReview, Arguments: []byte(`{}`)}); err != nil {
			return nil, err
		}
		if _, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "6", Name: ctrl.ToolApprove, Arguments: []byte(`{"summary":"final"}`)}); err != nil {
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
	// No add-all expected — commits are done by the agent now.
	if containsCallPrefix(git.calls, "add-all") {
		t.Fatalf("unexpected add-all in: %#v", git.calls)
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

func TestPipelineDetectsNoOpFix(t *testing.T) {
	tmp := t.TempDir()
	changeDir := filepath.Join(tmp, "specs", "changes", "noop-fix")
	mustWriteFile(t, filepath.Join(changeDir, "tasks.md"), "# Tasks\n\n## Phase 1: Fix test\n- [ ] Task one\n")
	mustWriteFile(t, filepath.Join(changeDir, "proposal.md"), "proposal\n")

	// fakeGit returns the same diff every time — simulates an agent that claims
	// to fix but doesn't actually change any files.
	git := &fakeGit{changedFiles: []string{"file.go"}, stagedFiles: []string{"file.go"}, diff: "diff --git a/file b/file"}

	var fixContent string
	controller := &fakeController{phaseFn: func(ctx context.Context, request ctrl.PhaseRequest, dispatcher ctrl.Dispatcher) (*ctrl.Result, error) {
		// Apply
		if _, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "1", Name: ctrl.ToolSubmitPrompt, Arguments: []byte(`{"prompt":"apply"}`)}); err != nil {
			return nil, err
		}
		// Review (fails to trigger fix cycle)
		if _, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "2", Name: ctrl.ToolRunReview, Arguments: []byte(`{}`)}); err != nil {
			return nil, err
		}
		// Fix — agent reports success but diff is unchanged
		fixResult, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "3", Name: ctrl.ToolSubmitPrompt, Arguments: []byte(`{"prompt":"fix the issue"}`)})
		if err != nil {
			return nil, err
		}
		fixContent = fixResult.Content

		// Review again
		if _, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "4", Name: ctrl.ToolRunReview, Arguments: []byte(`{}`)}); err != nil {
			return nil, err
		}
		// Approve
		approve, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "5", Name: ctrl.ToolApprove, Arguments: []byte(`{"summary":"passed"}`)})
		if err != nil {
			return nil, err
		}
		return &ctrl.Result{Summary: approve.Summary}, nil
	}, finalFn: func(ctx context.Context, request ctrl.FinalReviewRequest, dispatcher ctrl.Dispatcher) (*ctrl.Result, error) {
		if _, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "6", Name: ctrl.ToolRunReview, Arguments: []byte(`{}`)}); err != nil {
			return nil, err
		}
		approve, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "7", Name: ctrl.ToolApprove, Arguments: []byte(`{"summary":"final"}`)})
		if err != nil {
			return nil, err
		}
		return &ctrl.Result{Summary: approve.Summary}, nil
	}}

	p := &Pipeline{
		ChangePath:    changeDir,
		RepoDir:       tmp,
		Agent:         &fakeAgent{},
		Controller:    controller,
		Git:           git,
		PR:            &fakePR{note: "skipped"},
		MaxAttempts:   3,
		Config:        config.Config{Steps: config.DefaultConfig().Steps},
		processExists: func(pid int) bool { return false },
		now:           time.Now,
	}

	report, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !report.Phases[0].Passed {
		t.Fatalf("phase should still pass (controller approved after retry)")
	}

	// The key assertion: the fix step must have detected the no-op.
	if !strings.Contains(fixContent, "no file changes were detected") {
		t.Fatalf("fix dispatch content should warn about no-op fix, got: %q", fixContent)
	}

	// Verify 2 diff calls (pre-fix + post-fix).
	diffCount := 0
	for _, call := range git.calls {
		if call == "diff" {
			diffCount++
		}
	}
	if diffCount < 2 {
		t.Fatalf("expected at least 2 diff calls for fix verification, got %d", diffCount)
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

type fakeRPC struct {
	sessionIDs map[agent.Step]string
}

func (f *fakeRPC) Name() string                                 { return "fake-rpc" }
func (f *fakeRPC) CheckPrerequisites(ctx context.Context) error { return nil }
func (f *fakeRPC) ExecuteStep(ctx context.Context, step agent.Step, prompt, model string) (*agent.Result, error) {
	return nil, fmt.Errorf("unexpected ExecuteStep call")
}
func (f *fakeRPC) Cleanup(ctx context.Context) error { return nil }
func (f *fakeRPC) AsRPC() agent.RPCAgent             { return f }
func (f *fakeRPC) Events() <-chan agent.Event        { return nil }
func (f *fakeRPC) StartImplementationSession(ctx context.Context) (string, error) {
	return f.sessionIDs[agent.StepApply], nil
}
func (f *fakeRPC) StartReviewSession(ctx context.Context) (string, error) {
	return f.sessionIDs[agent.StepReview], nil
}
func (f *fakeRPC) SendMessage(ctx context.Context, sessionID, prompt, model string) (*agent.Result, error) {
	return nil, fmt.Errorf("unexpected SendMessage call")
}
func (f *fakeRPC) SessionForStep(ctx context.Context, step agent.Step) (string, error) {
	return f.sessionIDs[step], nil
}
func (f *fakeRPC) Abort(ctx context.Context, sessionID string) error { return nil }
func (f *fakeRPC) Close() error                                      { return nil }

func TestPipelineEmitsTraceEventsInOrder(t *testing.T) {
	tmp := t.TempDir()
	changeDir := filepath.Join(tmp, "specs", "changes", "trace-order")
	mustWriteFile(t, filepath.Join(changeDir, "tasks.md"), "# Tasks\n\n## Phase 1: Foundation\n- [ ] Task one\n")
	mustWriteFile(t, filepath.Join(changeDir, "proposal.md"), "proposal\n")

	fa := &fakeAgent{
		results: []agent.Result{
			{Stdout: "apply ok", ExitCode: 0, Duration: 10 * time.Millisecond},
			{Stdout: "no issues found", ExitCode: 0, Duration: 20 * time.Millisecond},
			{Stdout: "committed", ExitCode: 0, Duration: 5 * time.Millisecond},
			{Stdout: "approved", ExitCode: 0, Duration: 30 * time.Millisecond},
		},
		rpc: &fakeRPC{sessionIDs: map[agent.Step]string{
			agent.StepApply:       "impl-session",
			agent.StepReview:      "review-session",
			agent.StepFinalReview: "final-session",
			agent.StepCommit:      "commit-session",
		}},
	}
	git := &fakeGit{changedFiles: []string{"internal/pipeline/pipeline.go"}, stagedFiles: []string{"internal/pipeline/pipeline.go"}}
	pr := &fakePR{url: "https://example.test/pr/trace-order"}
	controller := &fakeController{phaseFn: func(ctx context.Context, request ctrl.PhaseRequest, dispatcher ctrl.Dispatcher) (*ctrl.Result, error) {
		if _, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "1", Name: ctrl.ToolSubmitPrompt, Arguments: []byte(`{"prompt":"apply this phase"}`)}); err != nil {
			return nil, err
		}
		if _, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "2", Name: ctrl.ToolRunReview, Arguments: []byte(`{}`)}); err != nil {
			return nil, err
		}
		if _, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "3", Name: ctrl.ToolCommit, Arguments: []byte(`{"message":"feat(trace-order): complete phase 1"}`)}); err != nil {
			return nil, err
		}
		if _, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "4", Name: ctrl.ToolApprove, Arguments: []byte(`{"summary":"phase passed"}`)}); err != nil {
			return nil, err
		}
		return &ctrl.Result{Summary: "phase passed"}, nil
	}, finalFn: func(ctx context.Context, request ctrl.FinalReviewRequest, dispatcher ctrl.Dispatcher) (*ctrl.Result, error) {
		if _, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "5", Name: ctrl.ToolRunReview, Arguments: []byte(`{}`)}); err != nil {
			return nil, err
		}
		if _, err := dispatcher.Dispatch(ctx, ctrl.ToolCall{ID: "6", Name: ctrl.ToolApprove, Arguments: []byte(`{"summary":"final passed"}`)}); err != nil {
			return nil, err
		}
		return &ctrl.Result{Summary: "final passed"}, nil
	}}

	var events []Event
	p := &Pipeline{
		ChangePath:  changeDir,
		RepoDir:     tmp,
		Agent:       fa,
		Controller:  controller,
		Git:         git,
		PR:          pr,
		MaxAttempts: 3,
		Config:      config.Config{Steps: config.DefaultConfig().Steps},
		AgentName:   "pi",
		OnEvent: func(msg Event) {
			events = append(events, msg)
		},
		processExists: func(pid int) bool { return false },
		now:           time.Now,
	}

	if _, err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	assertEventTypeSubsequence(t, events,
		"pipeline.StateEvent",                    // Init
		"pipeline.StateEvent",                    // Branch
		"pipeline.StateEvent",                    // PhaseLoop
		"pipeline.PhaseStartEvent",
		"pipeline.ControllerActivityEvent",        // analyzing
		"pipeline.ControllerToolCallStartEvent",   // submit_prompt
		"pipeline.ExecutionContextEvent",
		"pipeline.AgentInvokeEvent",
		"pipeline.AgentResultEvent",
		"pipeline.OutputEvent",
		"pipeline.ControllerToolCallResultEvent",
		"pipeline.ControllerActivityEvent",        // analyzing
		"pipeline.ControllerToolCallStartEvent",   // run_review
		"pipeline.ExecutionContextEvent",
		"pipeline.AgentInvokeEvent",
		"pipeline.AgentResultEvent",
		"pipeline.OutputEvent",
		"pipeline.ReviewResultEvent",
		"pipeline.ControllerToolCallResultEvent",
		"pipeline.ControllerActivityEvent",        // analyzing
		"pipeline.ControllerToolCallStartEvent",   // commit
		"pipeline.GitCommitEvent",                 // from commitViaAgent
		"pipeline.ControllerToolCallResultEvent",
		"pipeline.ControllerActivityEvent",        // analyzing
		"pipeline.ControllerToolCallStartEvent",   // approve
		"pipeline.ControllerToolCallResultEvent",
		"pipeline.ControllerActivityEvent",        // done
		"pipeline.PhaseResultEvent",
		"pipeline.StateEvent",                     // FinalReview
		"pipeline.ControllerActivityEvent",
		"pipeline.ControllerToolCallStartEvent",   // run_review
		"pipeline.ExecutionContextEvent",
		"pipeline.AgentInvokeEvent",
		"pipeline.AgentResultEvent",
		"pipeline.OutputEvent",
		"pipeline.ReviewResultEvent",
		"pipeline.ControllerToolCallResultEvent",
		"pipeline.ControllerActivityEvent",        // analyzing
		"pipeline.ControllerToolCallStartEvent",   // approve
		"pipeline.ControllerToolCallResultEvent",
		"pipeline.ControllerActivityEvent",        // done
		"pipeline.StateEvent",                     // PR
		"pipeline.GitPushEvent",
		"pipeline.PREvent",
		"pipeline.SummaryEvent",
		"pipeline.StateEvent",                     // Done
	)

	phaseReview := findEvent[ReviewResultEvent](t, events, func(e ReviewResultEvent) bool { return e.Step == "phase" })
	if !phaseReview.Passed || phaseReview.Phase != 1 || phaseReview.Attempt != 1 {
		t.Fatalf("unexpected phase review event: %+v", phaseReview)
	}
	finalReview := findEvent[ReviewResultEvent](t, events, func(e ReviewResultEvent) bool { return e.Step == "final" })
	if !finalReview.Passed || finalReview.Phase != 0 || finalReview.Attempt != 1 {
		t.Fatalf("unexpected final review event: %+v", finalReview)
	}
	applyInvoke := findEvent[AgentInvokeEvent](t, events, func(e AgentInvokeEvent) bool { return e.Step == string(agent.StepApply) })
	if applyInvoke.SessionID != "impl-session" || applyInvoke.WorkDir != tmp {
		t.Fatalf("unexpected apply invoke event: %+v", applyInvoke)
	}
	pushEvent := findEvent[GitPushEvent](t, events, func(e GitPushEvent) bool { return true })
	if pushEvent.Remote != "origin" || pushEvent.Branch != "change/trace-order" {
		t.Fatalf("unexpected push event: %+v", pushEvent)
	}
	prEvent := findEvent[PREvent](t, events, func(e PREvent) bool { return true })
	if prEvent.URL != "https://example.test/pr/trace-order" {
		t.Fatalf("unexpected PR event: %+v", prEvent)
	}
}

func assertEventTypeSubsequence(t *testing.T, events []Event, want ...string) {
	t.Helper()
	got := make([]string, 0, len(events))
	for _, event := range events {
		got = append(got, fmt.Sprintf("%T", event))
	}
	idx := 0
	for _, eventType := range got {
		if idx < len(want) && eventType == want[idx] {
			idx++
		}
	}
	if idx != len(want) {
		t.Fatalf("event subsequence = %#v, want %#v", got, want)
	}
}

func findEvent[T any](t *testing.T, events []Event, match func(T) bool) T {
	t.Helper()
	for _, event := range events {
		typed, ok := event.(T)
		if ok && match(typed) {
			return typed
		}
	}
	var zero T
	t.Fatalf("event %T not found", zero)
	return zero
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
