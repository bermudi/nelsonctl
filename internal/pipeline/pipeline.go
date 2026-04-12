package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bermudi/nelsonctl/internal/agent"
	"github.com/bermudi/nelsonctl/internal/config"
	ctrl "github.com/bermudi/nelsonctl/internal/controller"
	gitops "github.com/bermudi/nelsonctl/internal/git"
)

// GitOps captures the git operations used by the pipeline.
type GitOps interface {
	IsClean(ctx context.Context) error
	CurrentBranch(ctx context.Context) (string, error)
	Diff(ctx context.Context) (string, error)
	HasTrackedChanges(ctx context.Context) (bool, error)
	ChangedFiles(ctx context.Context) ([]string, error)
	StagedFiles(ctx context.Context) ([]string, error)
	BranchExists(ctx context.Context, branch string) (bool, error)
	CreateBranch(ctx context.Context, branch string) error
	Checkout(ctx context.Context, branch string) error
	Add(ctx context.Context, paths ...string) error
	AddAll(ctx context.Context) error
	Commit(ctx context.Context, subject, body string) error
	Push(ctx context.Context, remote, branch string, setUpstream bool) error
}

// State describes the coarse pipeline stage.
type State string

const (
	StateInit            State = "Init"
	StateBranch      State = "Branch"
	StatePhaseLoop   State = "PhaseLoop"
	StateFinalReview     State = "FinalReview"
	StatePR              State = "PR"
	StateDone            State = "Done"
)

// PhaseReport summarizes a phase execution.
type PhaseReport struct {
	Phase        Phase
	Attempts     int
	Passed       bool
	ReviewOutput string
	Summary      string
}

// Report summarizes a full pipeline run.
type Report struct {
	ChangeName          string
	BranchName          string
	States              []State
	Phases              []PhaseReport
	FinalReviewPassed   bool
	FinalReviewAttempts int
	FinalReviewOutput   string
	FinalReviewSummary  string
	PullRequestNote     string
	Resumed             bool
	Mode                config.ExecutionMode
	AgentName           string
	TotalAttempts       int
}

// Event is a sealed interface for all pipeline events.
type Event interface{ pipelineEvent() }

func (StateEvent) pipelineEvent()              {}
func (PhaseStartEvent) pipelineEvent()         {}
func (PhaseResultEvent) pipelineEvent()        {}
func (ExecutionContextEvent) pipelineEvent()   {}
func (ControllerActivityEvent) pipelineEvent() {}
func (OutputEvent) pipelineEvent()             {}
func (TauntEvent) pipelineEvent()              {}
func (SummaryEvent) pipelineEvent()            {}

// EventHandler receives pipeline events for display (e.g. TUI messages).
type EventHandler func(msg Event)

// Pipeline coordinates apply/review/fix across a change directory.
type Pipeline struct {
	ChangePath          string
	RepoDir             string
	Agent               agent.Agent
	Controller          ctrl.Controller
	Config              config.Config
	Git                 GitOps
	PR                  PullRequestCreator
	MaxAttempts         int
	OnEvent             EventHandler
	Confirm             func(prompt string) bool
	PauseChan           <-chan struct{}
	ResumeChan          <-chan struct{}
	Mode                config.ExecutionMode
	AgentName           string
	UseAgentEventOutput bool
	SkipPush            bool

	processExists func(pid int) bool
	now           func() time.Time
}

type lockFile struct {
	PID       int       `json:"pid"`
	Timestamp time.Time `json:"timestamp"`
}

type phaseExecution struct {
	pipeline         *Pipeline
	changeName       string
	phase            Phase
	final            bool
	maxAttempts      int
	currentAttempt   int
	lastReviewOutput string
	approvedSummary  string
	reviewCompleted  bool
	exhaustedPending bool
}

// New creates a pipeline with sensible defaults.
func New(changePath, repoDir string, a agent.Agent, git GitOps) *Pipeline {
	return &Pipeline{ChangePath: changePath, RepoDir: repoDir, Agent: a, Git: git, MaxAttempts: 3}
}

// Run executes the phase workflow for the configured change directory.
func (p *Pipeline) Run(ctx context.Context) (*Report, error) {
	if p.Agent == nil {
		return nil, fmt.Errorf("agent is required")
	}
	if p.Controller == nil {
		return nil, fmt.Errorf("controller is required")
	}
	if p.Git == nil {
		return nil, fmt.Errorf("git client is required")
	}
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = 3
	}
	if p.PR == nil {
		p.PR = NewGHClient()
	}
	if p.processExists == nil {
		p.processExists = gitops.ProcessExists
	}
	if p.now == nil {
		p.now = time.Now
	}

	changeName := ChangeNameFromPath(p.ChangePath)
	branchName := branchForChange(changeName)
	report := &Report{ChangeName: changeName, BranchName: branchName}
	report.Mode = p.Mode
	report.AgentName = p.AgentName

	phases, err := ParseTasksFile(filepath.Join(p.ChangePath, "tasks.md"))
	if err != nil {
		return report, err
	}
	remaining := RemainingPhases(phases)
	if len(remaining) != len(phases) {
		report.Resumed = len(remaining) > 0
	}
	p.emit(ExecutionContextEvent{Mode: p.Mode, Agent: p.AgentName, Resumed: report.Resumed})

	emitState := func(event StateEvent) {
		state := event.State
		report.States = append(report.States, state)
		p.emit(event)
	}

	emitState(StateEvent{State: StateInit})
	lockPath := filepath.Join(p.ChangePath, ".nelsonctl.lock")
	unlock, err := p.acquireLock(lockPath)
	if err != nil {
		return report, err
	}
	defer func() { _ = unlock() }()

	start := time.Now()

	branchReused, err := p.prepareBranch(ctx, branchName, report.Resumed)
	if err != nil {
		return report, err
	}
	emitState(StateEvent{State: StateBranch, BranchReused: branchReused})
	if err := p.createRecoveryCommit(ctx); err != nil {
		return report, err
	}

	emitState(StateEvent{State: StatePhaseLoop})
	for _, phase := range remaining {
		p.waitIfPaused(ctx)
		phaseReport, err := p.runPhase(ctx, changeName, phase)
		report.Phases = append(report.Phases, phaseReport)
		if err != nil {
			return report, err
		}
		if !phaseReport.Passed {
			break
		}
	}

	var phasesCompleted, phasesFailed int
	for _, pr := range report.Phases {
		if pr.Passed {
			phasesCompleted++
		} else {
			phasesFailed++
		}
		report.TotalAttempts += pr.Attempts
	}

	if phasesFailed == 0 {
		emitState(StateEvent{State: StateFinalReview})
		p.waitIfPaused(ctx)
		passed, attempts, summary, output, err := p.runFinalReview(ctx, changeName)
		if err != nil {
			return report, err
		}
		report.FinalReviewPassed = passed
		report.FinalReviewAttempts = attempts
		report.FinalReviewSummary = summary
		report.FinalReviewOutput = output
		report.TotalAttempts += attempts

		if report.FinalReviewPassed {
			emitState(StateEvent{State: StatePR})
			if !p.SkipPush {
				if err := p.Git.Push(ctx, "origin", branchName, true); err != nil {
					return report, err
				}
				p.emit(GitPushEvent{Remote: "origin", Branch: branchName})
			}
			note, prURL, prErr := p.PR.Create(ctx, p.ChangePath, changeName, filepath.Join(p.ChangePath, "proposal.md"))
			report.PullRequestNote = note
			if prErr != nil {
				return report, prErr
			}
			p.emit(PREvent{Title: changeName, URL: prURL})
		}
	}

	p.emit(SummaryEvent{
		PhasesCompleted: phasesCompleted,
		PhasesFailed:    phasesFailed,
		TotalAttempts:   report.TotalAttempts,
		Duration:        time.Since(start).Round(time.Millisecond).String(),
		Branch:          branchName,
		Mode:            p.Mode,
		Resumed:         report.Resumed,
	})

	emitState(StateEvent{State: StateDone})
	return report, nil
}

func (p *Pipeline) runPhase(ctx context.Context, changeName string, phase Phase) (PhaseReport, error) {
	execution := &phaseExecution{pipeline: p, changeName: changeName, phase: phase, maxAttempts: p.MaxAttempts, currentAttempt: 1}
	p.emit(PhaseStartEvent{Number: phase.Number, Name: phase.Name, Attempt: execution.currentAttempt})

	request := ctrl.PhaseRequest{
		ChangeName:   changeName,
		ReviewFailOn: p.Config.Review.FailOn,
		Phase:        controllerPhase(phase),
	}
	dispatcher := execution.dispatcher(false)

	result, err := p.Controller.RunPhase(ctx, request, dispatcher)
	if err != nil {
		if strings.Contains(err.Error(), "attempt budget exhausted") {
			return PhaseReport{Phase: phase, Attempts: execution.currentAttempt, Passed: false, ReviewOutput: execution.lastReviewOutput}, nil
		}
		return PhaseReport{Phase: phase, Attempts: execution.currentAttempt, ReviewOutput: execution.lastReviewOutput}, err
	}

	report := PhaseReport{
		Phase:        phase,
		Attempts:     execution.currentAttempt,
		Passed:       true,
		ReviewOutput: execution.lastReviewOutput,
		Summary:      strings.TrimSpace(result.Summary),
	}
	p.emit(PhaseResultEvent{Number: phase.Number, Passed: true, Attempts: report.Attempts, Review: report.ReviewOutput})

	return report, nil
}

func (p *Pipeline) runFinalReview(ctx context.Context, changeName string) (bool, int, string, string, error) {
	execution := &phaseExecution{pipeline: p, changeName: changeName, final: true, maxAttempts: p.MaxAttempts, currentAttempt: 1}
	request := ctrl.FinalReviewRequest{ChangeName: changeName, ReviewFailOn: p.Config.Review.FailOn}
	dispatcher := execution.dispatcher(true)
	p.emit(ControllerActivityEvent{Analyzing: true})

	result, err := p.Controller.RunFinalReview(ctx, request, dispatcher)
	if err != nil {
		if strings.Contains(err.Error(), "attempt budget exhausted") {
			return false, execution.currentAttempt, "", execution.lastReviewOutput, nil
		}
		return false, execution.currentAttempt, "", execution.lastReviewOutput, err
	}
	return true, execution.currentAttempt, strings.TrimSpace(result.Summary), execution.lastReviewOutput, nil
}

func (e *phaseExecution) dispatcher(final bool) ctrl.Dispatcher {
	base := ctrl.NewToolDispatcher(ctrl.Handlers{
		RepoDir: e.pipeline.RepoDir,
		GetDiff: func(ctx context.Context) (string, error) {
			return e.pipeline.Git.Diff(ctx)
		},
		SubmitPrompt: func(ctx context.Context, prompt string) (string, error) {
			return e.submitPrompt(ctx, prompt, final)
		},
		AfterSubmitPrompt: e.afterSubmitPrompt,
		RunReview: func(ctx context.Context) (string, error) {
			return e.runReview(ctx, final)
		},
		Approve: func(ctx context.Context, summary string) error {
			e.approvedSummary = strings.TrimSpace(summary)
			e.exhaustedPending = false
			return nil
		},
		Commit: func(ctx context.Context, message string) error {
			return e.commitViaAgent(ctx, message)
		},
		OnToolCallStart: func(call ctrl.ToolCall) {
			e.pipeline.emit(ControllerToolCallStartEvent{ID: call.ID, Tool: string(call.Name), Arguments: append(json.RawMessage(nil), call.Arguments...)})
		},
		OnToolCallResult: func(call ctrl.ToolCall, result ctrl.DispatchResult, err error) {
			event := ControllerToolCallResultEvent{
				ID:             call.ID,
				Tool:           string(call.Name),
				Approved:       result.Approved,
				Summary:        result.Summary,
				ContentLen:     len(result.Content),
				UserMessageLen: len(result.UserMessage),
			}
			if err != nil {
				event.Error = err.Error()
			}
			e.pipeline.emit(event)
		},
	})
	return controllerEventDispatcher{pipeline: e.pipeline, inner: base}
}

func (e *phaseExecution) submitPrompt(ctx context.Context, prompt string, final bool) (string, error) {
	if e.exhaustedPending {
		if !e.final {
			e.pipeline.emit(PhaseResultEvent{Number: e.phase.Number, Passed: false, Attempts: e.currentAttempt, Review: e.lastReviewOutput})
			e.pipeline.emit(TauntEvent{PhaseNumber: e.phase.Number})
		}
		return "", fmt.Errorf("attempt budget exhausted after attempt %d", e.currentAttempt)
	}
	if e.reviewCompleted {
		e.reviewCompleted = false
		e.currentAttempt++
		if !e.final {
			e.pipeline.emit(PhaseResultEvent{Number: e.phase.Number, Passed: false, Attempts: e.currentAttempt - 1, Review: e.lastReviewOutput})
			e.pipeline.emit(PhaseStartEvent{Number: e.phase.Number, Name: e.phase.Name, Attempt: e.currentAttempt})
		}
	}
	step := agent.StepApply
	model := e.pipeline.Config.Steps.Apply.Model
	if e.currentAttempt > 1 || (final && e.lastReviewOutput != "") {
		step = agent.StepFix
		model = e.pipeline.Config.Steps.Fix.Model
	}

	// Capture pre-fix diff so we can verify the agent actually changed something.
	var preFixDiff string
	preDiffOK := false
	if step == agent.StepFix {
		if diff, err := e.pipeline.Git.Diff(ctx); err == nil {
			preFixDiff = diff
			preDiffOK = true
		}
	}

	trimmedPrompt := strings.TrimSpace(prompt)
	e.pipeline.emit(ExecutionContextEvent{Mode: e.pipeline.Mode, Agent: e.pipeline.AgentName, Step: string(step), Model: model, Resumed: false})
	e.pipeline.emit(AgentInvokeEvent{Agent: e.pipeline.AgentName, Step: string(step), Model: model, SessionID: sessionIDForStep(ctx, e.pipeline.Agent, step), Prompt: trimmedPrompt, WorkDir: e.pipeline.RepoDir})
	start := time.Now()
	res, err := e.pipeline.Agent.ExecuteStep(ctx, step, trimmedPrompt, model)
	e.pipeline.emitAgentResult(res, err, time.Since(start))
	e.pipeline.emitResultOutput(res)
	if err != nil {
		return fmt.Sprintf("Agent step %s failed: %s", step, strings.TrimSpace(reviewOutput(resultText(res), errorText(err)))), nil
	}
	if resultExitCode(res, nil) != 0 {
		return fmt.Sprintf("Agent step %s exited with code %d. Output:\n%s", step, resultExitCode(res, nil), strings.TrimSpace(reviewOutput(resultText(res), ""))), nil
	}

	// Post-fix verification: detect no-op fixes where the agent reported success
	// but didn't actually change any files. This catches the common failure mode
	// where the LLM describes what it would do without executing the edits.
	if step == agent.StepFix && preDiffOK {
		if postDiff, err := e.pipeline.Git.Diff(ctx); err == nil && postDiff == preFixDiff {
			return "Agent reported success but no file changes were detected since the last attempt. The fix may not have been applied. Consider inspecting the current diff with get_diff and retrying with a more explicit prompt.", nil
		}
	}

	return "Agent completed successfully.", nil
}

func (e *phaseExecution) runReview(ctx context.Context, final bool) (string, error) {
	step := agent.StepReview
	if final {
		step = agent.StepFinalReview
	}
	prompt := MechanicalReviewPrompt(e.changeName, final)
	model := e.pipeline.Config.Steps.Review.Model
	e.pipeline.emit(ExecutionContextEvent{Mode: e.pipeline.Mode, Agent: e.pipeline.AgentName, Step: string(step), Model: model, Resumed: false})
	e.pipeline.emit(AgentInvokeEvent{Agent: e.pipeline.AgentName, Step: string(step), Model: model, SessionID: sessionIDForStep(ctx, e.pipeline.Agent, step), Prompt: prompt, WorkDir: e.pipeline.RepoDir})
	start := time.Now()
	res, err := e.pipeline.Agent.ExecuteStep(ctx, step, prompt, model)
	e.pipeline.emitAgentResult(res, err, time.Since(start))
	e.pipeline.emitResultOutput(res)
	output := reviewOutput(resultText(res), errorText(err))
	e.lastReviewOutput = output
	e.reviewCompleted = true
	e.exhaustedPending = e.currentAttempt >= e.maxAttempts
	passed := ReviewPassed(output, resultExitCode(res, err))
	if final {
		e.pipeline.emit(ReviewResultEvent{Passed: passed, Output: output, Attempt: e.currentAttempt, Step: "final"})
	} else {
		e.pipeline.emit(ReviewResultEvent{Passed: passed, Output: output, Attempt: e.currentAttempt, Step: "phase", Phase: e.phase.Number})
	}
	if err != nil {
		return output, nil
	}
	if resultExitCode(res, nil) != 0 {
		return output, nil
	}
	return output, nil
}

func (e *phaseExecution) afterSubmitPrompt() string {
	if strings.TrimSpace(e.approvedSummary) != "" {
		return ""
	}
	if e.currentAttempt == 1 {
		return fmt.Sprintf("After the implementation completes, call run_review next. Attempt %d of %d is in progress.", e.currentAttempt, e.maxAttempts)
	}
	return fmt.Sprintf("Attempt %d of %d. After the fix completes, call run_review next.", e.currentAttempt, e.maxAttempts)
}

func (p *Pipeline) prepareBranch(ctx context.Context, branchName string, resumed bool) (bool, error) {
	currentBranch, err := p.Git.CurrentBranch(ctx)
	if err != nil {
		return false, fmt.Errorf("detect current branch: %w", err)
	}
	exists, err := p.Git.BranchExists(ctx, branchName)
	if err != nil {
		return false, fmt.Errorf("check branch existence: %w", err)
	}
	if !exists {
		if err := p.Git.IsClean(ctx); err != nil {
			return false, fmt.Errorf("worktree has uncommitted changes, commit or stash before running: %w", err)
		}
		return false, p.Git.CreateBranch(ctx, branchName)
	}
	if currentBranch == branchName {
		return true, nil
	}
	if err := p.Git.IsClean(ctx); err != nil {
		return false, fmt.Errorf("worktree has uncommitted changes on unrelated branch %s; commit or stash before running: %w", currentBranch, err)
	}
	if resumed && p.Confirm != nil && !p.Confirm(fmt.Sprintf("Resume existing branch %s?", branchName)) {
		return false, fmt.Errorf("branch %s already exists; resume was declined", branchName)
	}
	return true, p.Git.Checkout(ctx, branchName)
}

func (p *Pipeline) createRecoveryCommit(ctx context.Context) error {
	hasChanges, err := p.Git.HasTrackedChanges(ctx)
	if err != nil {
		return fmt.Errorf("check tracked changes: %w", err)
	}
	if !hasChanges {
		return nil
	}
	files, err := p.Git.ChangedFiles(ctx)
	if err != nil {
		return fmt.Errorf("list changed files for recovery commit: %w", err)
	}
	if len(files) == 0 {
		return nil
	}
	if err := p.Git.Add(ctx, files...); err != nil {
		return err
	}
	return p.Git.Commit(ctx, "chore: recovery commit", "Preserve resumable tracked work before continuing the controller pipeline.")
}

func (p *Pipeline) acquireLock(path string) (func() error, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create lock directory: %w", err)
	}
	if data, err := os.ReadFile(path); err == nil {
		var existing lockFile
		if json.Unmarshal(data, &existing) == nil && p.processExists(existing.PID) {
			return nil, fmt.Errorf("change is already in progress (lock held by pid %d)", existing.PID)
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("remove stale lock: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read lock file: %w", err)
	}
	// Write .gitignore alongside the lock so AddAll never stages the lock file.
	gitignorePath := filepath.Join(filepath.Dir(path), ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte(".nelsonctl.lock\n"), 0o644); err != nil {
		return nil, fmt.Errorf("write lock gitignore: %w", err)
	}
	data, err := json.Marshal(lockFile{PID: os.Getpid(), Timestamp: p.now().UTC()})
	if err != nil {
		return nil, fmt.Errorf("marshal lock file: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return nil, fmt.Errorf("write lock file: %w", err)
	}
	return func() error {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}, nil
}

func (e *phaseExecution) commitViaAgent(ctx context.Context, message string) error {
	// Delegate commit to the agent: it stages files, writes .gitignore,
	// and commits. This avoids nelsonctl having to know about
	// node_modules, dist/, build artifacts, etc.
	agentPrompt := fmt.Sprintf("Stage all your changes (ensure appropriate .gitignore entries for node_modules, dist, .env, build artifacts, etc.) and commit with this message:\n\n%s", message)
	result, err := e.pipeline.Agent.ExecuteStep(ctx, agent.StepCommit, agentPrompt, e.pipeline.Config.Steps.Apply.Model)
	if err != nil {
		return fmt.Errorf("agent commit: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("agent commit failed (exit %d): %s", result.ExitCode, result.Stderr)
	}
	e.pipeline.emit(GitCommitEvent{Subject: message})
	return nil
}

func phaseSummary(phases []Phase) string {
	var b strings.Builder
	for _, phase := range phases {
		fmt.Fprintf(&b, "Phase %d: %s\n", phase.Number, phase.Name)
	}
	return strings.TrimSpace(b.String())
}

func phaseBody(phase Phase) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Phase %d: %s\n", phase.Number, phase.Name)
	for _, task := range phase.Tasks {
		fmt.Fprintf(&b, "- [x] %s\n", task.Text)
	}
	return strings.TrimSpace(b.String())
}

func branchForChange(changeName string) string {
	return "change/" + changeName
}

func controllerPhase(phase Phase) ctrl.Phase {
	tasks := make([]string, 0, len(phase.Tasks))
	for _, task := range phase.Tasks {
		tasks = append(tasks, task.Text)
	}
	return ctrl.Phase{Number: phase.Number, Name: phase.Name, Tasks: tasks}
}

func reviewOutput(stdout, stderr string) string {
	stdout = strings.TrimSpace(stdout)
	stderr = strings.TrimSpace(stderr)
	if stdout == "" {
		return stderr
	}
	if stderr == "" {
		return stdout
	}
	return stdout + "\n" + stderr
}

func resultText(res *agent.Result) string {
	if res == nil {
		return ""
	}
	return reviewOutput(res.Stdout, res.Stderr)
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func resultExitCode(res *agent.Result, err error) int {
	if res != nil {
		return res.ExitCode
	}
	if err != nil {
		return 1
	}
	return 0
}

func (p *Pipeline) emitAgentResult(res *agent.Result, err error, duration time.Duration) {
	stdoutLen, stderrLen := 0, 0
	if res != nil {
		stdoutLen = len(res.Stdout)
		stderrLen = len(res.Stderr)
		if res.Duration > 0 {
			duration = res.Duration
		}
	}
	p.emit(AgentResultEvent{
		ExitCode:   resultExitCode(res, err),
		DurationMs: duration.Milliseconds(),
		StdoutLen:  stdoutLen,
		StderrLen:  stderrLen,
	})
}

func sessionIDForStep(ctx context.Context, a agent.Agent, step agent.Step) string {
	if step != agent.StepApply && step != agent.StepFix {
		return ""
	}
	rpc := a.AsRPC()
	if rpc == nil {
		return ""
	}
	sessionID, err := rpc.SessionForStep(ctx, step)
	if err != nil {
		return ""
	}
	return sessionID
}

func (p *Pipeline) emitResultOutput(res *agent.Result) {
	if p.UseAgentEventOutput {
		return
	}
	if res != nil && res.Stdout != "" {
		p.emit(OutputEvent{Chunk: res.Stdout})
	}
}

func (p *Pipeline) emit(msg Event) {
	if p.OnEvent != nil {
		p.OnEvent(msg)
	}
	if os.Getenv("NELSONCTL_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[DEBUG] %T %+v\n", msg, msg)
	}
}

func (p *Pipeline) waitIfPaused(ctx context.Context) {
	if p.PauseChan == nil {
		return
	}
	select {
	case <-p.PauseChan:
		if p.ResumeChan != nil {
			select {
			case <-p.ResumeChan:
			case <-ctx.Done():
			}
		}
	case <-ctx.Done():
	default:
	}
}

// StateEvent is emitted when the pipeline transitions states.
type StateEvent struct {
	State        State
	BranchReused bool
}

// PhaseStartEvent is emitted when a phase begins execution.
type PhaseStartEvent struct {
	Number  int
	Name    string
	Attempt int
}

// PhaseResultEvent is emitted when a phase completes.
type PhaseResultEvent struct {
	Number   int
	Passed   bool
	Attempts int
	Review   string
}

// ExecutionContextEvent updates the operator-visible step context.
type ExecutionContextEvent struct {
	Mode    config.ExecutionMode
	Agent   string
	Step    string
	Model   string
	Resumed bool
}

// ControllerActivityEvent surfaces controller tool activity without parsing reasoning.
type ControllerActivityEvent struct {
	Tool      string
	Summary   string
	Analyzing bool
}

// OutputEvent is emitted for agent output chunks.
type OutputEvent struct {
	Chunk string
}

// TauntEvent is emitted when a phase exhausts all retry attempts.
type TauntEvent struct {
	PhaseNumber int
}

// SummaryEvent is emitted when the pipeline finishes.
type SummaryEvent struct {
	PhasesCompleted int
	PhasesFailed    int
	TotalAttempts   int
	Duration        string
	Branch          string
	Mode            config.ExecutionMode
	Resumed         bool
}

type controllerEventDispatcher struct {
	pipeline *Pipeline
	inner    ctrl.Dispatcher
}

func (d controllerEventDispatcher) Dispatch(ctx context.Context, call ctrl.ToolCall) (ctrl.DispatchResult, error) {
	d.pipeline.emit(ControllerActivityEvent{Tool: string(call.Name)})
	result, err := d.inner.Dispatch(ctx, call)
	if err != nil {
		return result, err
	}
	if result.Approved {
		d.pipeline.emit(ControllerActivityEvent{Tool: string(call.Name), Summary: result.Summary})
		return result, nil
	}
	d.pipeline.emit(ControllerActivityEvent{Analyzing: true})
	return result, nil
}
