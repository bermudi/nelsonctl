package pipeline

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/bermudi/nelsonctl/internal/agent"
)

// GitOps captures the git operations used by the pipeline.
type GitOps interface {
	IsClean(ctx context.Context) error
	BranchExists(ctx context.Context, branch string) (bool, error)
	CreateBranch(ctx context.Context, branch string) error
	Checkout(ctx context.Context, branch string) error
	Add(ctx context.Context, paths ...string) error
	Commit(ctx context.Context, subject, body string) error
	Push(ctx context.Context, remote, branch string, setUpstream bool) error
}

// State describes the coarse pipeline stage.
type State string

const (
	StateInit            State = "Init"
	StateBranch          State = "Branch"
	StateCommitArtifacts State = "CommitArtifacts"
	StatePhaseLoop       State = "PhaseLoop"
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
}

// Report summarizes a full pipeline run.
type Report struct {
	ChangeName        string
	BranchName        string
	States            []State
	Phases            []PhaseReport
	FinalReviewPassed bool
	FinalReviewOutput string
	PullRequestNote   string
}

// Event is a sealed interface for all pipeline events.
type Event interface{ pipelineEvent() }

func (StateEvent) pipelineEvent()       {}
func (PhaseStartEvent) pipelineEvent()  {}
func (PhaseResultEvent) pipelineEvent() {}
func (OutputEvent) pipelineEvent()      {}
func (TauntEvent) pipelineEvent()       {}
func (SummaryEvent) pipelineEvent()     {}

// EventHandler receives pipeline events for display (e.g. TUI messages).
type EventHandler func(msg Event)

// Pipeline coordinates apply/review/fix across a change directory.
type Pipeline struct {
	ChangePath  string
	Agent       agent.Agent
	Git         GitOps
	PR          PullRequestCreator
	MaxAttempts int
	OnEvent     EventHandler
	Confirm     func(prompt string) bool
	PauseChan   <-chan struct{}
	ResumeChan  <-chan struct{}
}

// New creates a pipeline with sensible defaults.
func New(changePath string, a agent.Agent, git GitOps) *Pipeline {
	return &Pipeline{ChangePath: changePath, Agent: a, Git: git, MaxAttempts: 3}
}

// Run executes the phase workflow for the configured change directory.
func (p *Pipeline) Run(ctx context.Context) (*Report, error) {
	if p.Agent == nil {
		return nil, fmt.Errorf("agent is required")
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

	changeName := filepath.Base(filepath.Clean(p.ChangePath))
	branchName := branchForChange(changeName)
	report := &Report{ChangeName: changeName, BranchName: branchName}

	phases, err := ParseTasksFile(filepath.Join(p.ChangePath, "tasks.md"))
	if err != nil {
		return report, err
	}

	emitState := func(state State) {
		report.States = append(report.States, state)
		p.emit(StateEvent{State: state})
	}

	emitState(StateInit)

	start := time.Now()

	emitState(StateBranch)
	if err := p.Git.IsClean(ctx); err != nil {
		return report, fmt.Errorf("worktree has uncommitted changes, commit or stash before running: %w", err)
	}
	exists, err := p.Git.BranchExists(ctx, branchName)
	if err != nil {
		return report, fmt.Errorf("check branch existence: %w", err)
	}
	if exists {
		if p.Confirm == nil || !p.Confirm(fmt.Sprintf("Branch %s already exists. Reuse it?", branchName)) {
			return report, fmt.Errorf("branch %s already exists; reuse or delete it before running", branchName)
		}
		if err := p.Git.Checkout(ctx, branchName); err != nil {
			return report, fmt.Errorf("checkout existing branch %s: %w", branchName, err)
		}
	} else {
		if err := p.Git.CreateBranch(ctx, branchName); err != nil {
			return report, err
		}
	}

	emitState(StateCommitArtifacts)
	if err := p.commitArtifacts(ctx, changeName, phases); err != nil {
		return report, err
	}

	emitState(StatePhaseLoop)
	for _, phase := range phases {
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
	}

	if phasesFailed == 0 {
		emitState(StateFinalReview)
		p.waitIfPaused(ctx)
		for attempt := 1; attempt <= p.MaxAttempts; attempt++ {
			reviewRes, reviewErr := p.Agent.Run(ctx, FinalReviewPrompt(changeName), p.ChangePath)
			reviewOutput := ReviewOutput(resultText(reviewRes), errorText(reviewErr))
			reviewExit := resultExitCode(reviewRes, reviewErr)
			report.FinalReviewOutput = reviewOutput
			report.FinalReviewPassed = ReviewPassed(reviewOutput, reviewExit)

			if report.FinalReviewPassed {
				break
			}
			if attempt < p.MaxAttempts {
				fixRes, fixErr := p.Agent.Run(ctx, FixPrompt(reviewOutput), p.ChangePath)
				if fixRes != nil && fixRes.Stdout != "" {
					p.emit(OutputEvent{Chunk: fixRes.Stdout})
				}
				if fixErr != nil || resultExitCode(fixRes, fixErr) != 0 {
					continue
				}
			}
		}

		if report.FinalReviewPassed {
			emitState(StatePR)
			if err := p.Git.Push(ctx, "origin", branchName, true); err != nil {
				return report, err
			}
			note, prErr := p.PR.Create(ctx, p.ChangePath, changeName, filepath.Join(p.ChangePath, "proposal.md"))
			report.PullRequestNote = note
			if prErr != nil {
				return report, prErr
			}
		}
	}

	p.emit(SummaryEvent{
		PhasesCompleted: phasesCompleted,
		PhasesFailed:    phasesFailed,
		Duration:        time.Since(start).Round(time.Millisecond).String(),
		Branch:          branchName,
	})

	emitState(StateDone)
	return report, nil
}

func (p *Pipeline) runPhase(ctx context.Context, changeName string, phase Phase) (PhaseReport, error) {
	report := PhaseReport{Phase: phase}
	prompt := ApplyPrompt(changeName, phase)

	for attempt := 1; attempt <= p.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		report.Attempts = attempt
		p.emit(PhaseStartEvent{Number: phase.Number, Name: phase.Name, Attempt: attempt})

		applyRes, applyErr := p.Agent.Run(ctx, prompt, p.ChangePath)
		if applyRes != nil && applyRes.Stdout != "" {
			p.emit(OutputEvent{Chunk: applyRes.Stdout})
		}
		applyExit := resultExitCode(applyRes, applyErr)
		if applyErr != nil || applyExit != 0 {
			report.ReviewOutput = ReviewOutput(resultText(applyRes), errorText(applyErr))
			if attempt == p.MaxAttempts {
				break
			}
			prompt = FixPrompt(report.ReviewOutput)
			continue
		}

		reviewRes, reviewErr := p.Agent.Run(ctx, ReviewPrompt(changeName), p.ChangePath)
		reviewOutput := ReviewOutput(resultText(reviewRes), errorText(reviewErr))
		report.ReviewOutput = reviewOutput
		passed := ReviewPassed(reviewOutput, resultExitCode(reviewRes, reviewErr))
		p.emit(PhaseResultEvent{Number: phase.Number, Passed: passed, Attempts: attempt, Review: reviewOutput})
		if passed {
			report.Passed = true
			break
		}

		if attempt == p.MaxAttempts {
			break
		}
		prompt = FixPrompt(reviewOutput)
	}

	if !report.Passed && report.Attempts == p.MaxAttempts {
		p.emit(TauntEvent{PhaseNumber: phase.Number})
	}

	if report.Passed {
		if err := p.commitPhase(ctx, changeName, phase); err != nil {
			return report, err
		}
	}

	return report, nil
}

func (p *Pipeline) commitArtifacts(ctx context.Context, changeName string, phases []Phase) error {
	if err := p.Git.Add(ctx, p.ChangePath); err != nil {
		return err
	}
	body := fmt.Sprintf("Planning artifacts for %s\n\n%s", changeName, phaseSummary(phases))
	return p.Git.Commit(ctx, fmt.Sprintf("chore: add litespec artifacts for %s", changeName), body)
}

func (p *Pipeline) commitPhase(ctx context.Context, changeName string, phase Phase) error {
	if err := p.Git.Add(ctx, "."); err != nil {
		return err
	}
	subject := fmt.Sprintf("feat(%s): complete phase %d - %s", changeName, phase.Number, phase.Name)
	return p.Git.Commit(ctx, subject, phaseBody(phase))
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

func resultText(res *agent.Result) string {
	if res == nil {
		return ""
	}
	return ReviewOutput(res.Stdout, res.Stderr)
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

func (p *Pipeline) emit(msg Event) {
	if p.OnEvent != nil {
		p.OnEvent(msg)
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
	State State
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
	Duration        string
	Branch          string
}
