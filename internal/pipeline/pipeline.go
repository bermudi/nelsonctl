package pipeline

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bermudi/nelsonctl/internal/agent"
)

// GitOps captures the git operations used by the pipeline.
type GitOps interface {
	CreateBranch(ctx context.Context, branch string) error
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

// Pipeline coordinates apply/review/fix across a change directory.
type Pipeline struct {
	ChangePath  string
	Agent       agent.Agent
	Git         GitOps
	PR          PullRequestCreator
	MaxAttempts int
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
	}

	emitState(StateInit)
	emitState(StateBranch)
	if err := p.Git.CreateBranch(ctx, branchName); err != nil {
		return report, err
	}

	emitState(StateCommitArtifacts)
	if err := p.commitArtifacts(ctx, changeName, phases); err != nil {
		return report, err
	}

	emitState(StatePhaseLoop)
	for _, phase := range phases {
		phaseReport, err := p.runPhase(ctx, changeName, phase)
		report.Phases = append(report.Phases, phaseReport)
		if err != nil {
			return report, err
		}
	}

	emitState(StateFinalReview)
	finalRes, finalErr := p.Agent.Run(ctx, FinalReviewPrompt(changeName), p.ChangePath)
	report.FinalReviewOutput = ReviewOutput(resultText(finalRes), errorText(finalErr))
	report.FinalReviewPassed = ReviewPassed(report.FinalReviewOutput, resultExitCode(finalRes, finalErr))

	emitState(StatePR)
	if err := p.Git.Push(ctx, "origin", branchName, true); err != nil {
		return report, err
	}
	note, prErr := p.PR.Create(ctx, p.ChangePath, changeName, filepath.Join(p.ChangePath, "proposal.md"))
	report.PullRequestNote = note
	if prErr != nil {
		return report, prErr
	}

	emitState(StateDone)
	return report, nil
}

func (p *Pipeline) runPhase(ctx context.Context, changeName string, phase Phase) (PhaseReport, error) {
	report := PhaseReport{Phase: phase}
	prompt := ApplyPrompt(changeName, phase)

	for attempt := 1; attempt <= p.MaxAttempts; attempt++ {
		report.Attempts = attempt

		applyRes, applyErr := p.Agent.Run(ctx, prompt, p.ChangePath)
		if applyErr != nil {
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
		if ReviewPassed(reviewOutput, resultExitCode(reviewRes, reviewErr)) {
			report.Passed = true
			break
		}

		if attempt == p.MaxAttempts {
			break
		}
		prompt = FixPrompt(reviewOutput)
	}

	if err := p.commitPhase(ctx, phase); err != nil {
		return report, err
	}

	return report, nil
}

func (p *Pipeline) commitArtifacts(ctx context.Context, changeName string, phases []Phase) error {
	if err := p.Git.Add(ctx, p.ChangePath); err != nil {
		return err
	}
	body := fmt.Sprintf("Planning artifacts for %s\n\n%s", changeName, phaseSummary(phases))
	return p.Git.Commit(ctx, fmt.Sprintf("chore: add planning artifacts for %s", changeName), body)
}

func (p *Pipeline) commitPhase(ctx context.Context, phase Phase) error {
	if err := p.Git.Add(ctx, p.ChangePath); err != nil {
		return err
	}
	return p.Git.Commit(ctx, fmt.Sprintf("chore(phase-%d): %s", phase.Number, phase.Name), phaseBody(phase))
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
		fmt.Fprintf(&b, "- %s\n", task.Text)
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
