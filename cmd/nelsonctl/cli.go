package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/bermudi/nelsonctl/internal/agent"
	"github.com/bermudi/nelsonctl/internal/git"
	"github.com/bermudi/nelsonctl/internal/pipeline"
	"github.com/bermudi/nelsonctl/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

type options struct {
	agentName string
	timeout   time.Duration
	dryRun    bool
	noPR      bool
	verbose   bool
}

// runCLI parses flags and executes nelsonctl.
func runCLI(ctx context.Context, args []string, cwd string, stdout, stderr io.Writer) int {
	opts, changePath, err := parseArgs(args, stderr)
	if err != nil {
		return 2
	}

	absChangePath := changePath
	if !filepath.IsAbs(absChangePath) {
		absChangePath = filepath.Join(cwd, absChangePath)
	}

	if opts.dryRun {
		if err := printDryRun(stdout, changePath, absChangePath, opts.agentName); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}

	agentOptions := []agent.Option{agent.WithTimeout(opts.timeout)}
	if opts.verbose {
		agentOptions = append(agentOptions, agent.WithStdoutCallback(func(chunk []byte) {
			_, _ = stdout.Write(chunk)
		}))
	}

	agentClient, err := agent.New(opts.agentName, agentOptions...)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := agentClient.Available(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	gitClient := git.NewClient(cwd)
	p := pipeline.New(absChangePath, agentClient, gitClient)

	if !opts.noPR {
		p.PR = pipeline.NewGHClient()
	} else {
		p.PR = noopPullRequestCreator{}
	}

	if !opts.verbose && !opts.dryRun {
		events := make(chan tea.Msg, 64)
		phases, parseErr := pipeline.ParseTasksFile(filepath.Join(absChangePath, "tasks.md"))
		if parseErr != nil {
			fmt.Fprintln(stderr, parseErr)
			return 1
		}

		model := tui.NewModel(phases).WithEventChannel(events)
		p.OnEvent = func(msg pipeline.Event) {
			events <- toTeaMsg(msg)
		}
		p.PauseChan = model.PauseChan()
		p.ResumeChan = model.ResumeChan()

		type runResult struct {
			report *pipeline.Report
			err    error
		}
		resultCh := make(chan runResult, 1)

		go func() {
			report, runErr := p.Run(ctx)
			close(events)
			resultCh <- runResult{report: report, err: runErr}
		}()

		prog := tea.NewProgram(model, tea.WithAltScreen())
		if _, err := prog.Run(); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}

		result := <-resultCh
		if result.err != nil {
			fmt.Fprintln(stderr, result.err)
			return 1
		}
		return 0
	}

	report, err := p.Run(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	printRunSummary(stdout, report)
	if report.PullRequestNote != "" {
		fmt.Fprintln(stdout, report.PullRequestNote)
	}

	return 0
}

func parseArgs(args []string, stderr io.Writer) (options, string, error) {
	fs := flag.NewFlagSet("nelsonctl", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var opts options
	fs.StringVar(&opts.agentName, "agent", "opencode", "agent CLI to use")
	fs.DurationVar(&opts.timeout, "timeout", 10*time.Minute, "timeout per agent invocation")
	fs.BoolVar(&opts.dryRun, "dry-run", false, "show the pipeline plan without executing")
	fs.BoolVar(&opts.noPR, "no-pr", false, "skip pull request creation")
	fs.BoolVar(&opts.verbose, "verbose", false, "show full agent output")

	if err := fs.Parse(args); err != nil {
		return options{}, "", err
	}
	if fs.NArg() != 1 {
		return options{}, "", fmt.Errorf("change path is required")
	}

	return opts, fs.Arg(0), nil
}

func printDryRun(stdout io.Writer, changeArg, absChangePath, agentName string) error {
	phases, err := pipeline.ParseTasksFile(filepath.Join(absChangePath, "tasks.md"))
	if err != nil {
		return err
	}

	branch := "change/" + filepath.Base(filepath.Clean(absChangePath))
	fmt.Fprintf(stdout, "Dry run for %s\n", changeArg)
	fmt.Fprintf(stdout, "Agent: %s\n", agentName)
	fmt.Fprintf(stdout, "Branch: %s\n", branch)
	for _, phase := range phases {
		fmt.Fprintf(stdout, "\nPhase %d: %s\n", phase.Number, phase.Name)
		for _, task := range phase.Tasks {
			marker := " "
			if task.Done {
				marker = "x"
			}
			fmt.Fprintf(stdout, "  - [%s] %s\n", marker, task.Text)
		}
	}
	return nil
}

func printRunSummary(stdout io.Writer, report *pipeline.Report) {
	fmt.Fprintf(stdout, "branch: %s\n", report.BranchName)
	fmt.Fprintf(stdout, "states: %s\n", strings.Join(statesToStrings(report.States), " -> "))
	for _, phase := range report.Phases {
		fmt.Fprintf(stdout, "phase %d: attempts=%d passed=%t\n", phase.Phase.Number, phase.Attempts, phase.Passed)
	}
	fmt.Fprintf(stdout, "final review passed: %t\n", report.FinalReviewPassed)
}

func statesToStrings(states []pipeline.State) []string {
	out := make([]string, len(states))
	for i, state := range states {
		out[i] = string(state)
	}
	return out
}

type noopPullRequestCreator struct{}

func (noopPullRequestCreator) Create(ctx context.Context, repoDir, title, bodyFile string) (string, error) {
	return "PR creation skipped (--no-pr)", nil
}

func toTeaMsg(msg pipeline.Event) tea.Msg {
	switch m := msg.(type) {
	case pipeline.StateEvent:
		return tui.StateMsg{State: m.State}
	case pipeline.PhaseStartEvent:
		return tui.PhaseMsg{Number: m.Number, Name: m.Name, Attempt: m.Attempt}
	case pipeline.PhaseResultEvent:
		return tui.PhaseResultMsg{Number: m.Number, Passed: m.Passed, Attempts: m.Attempts, Review: m.Review}
	case pipeline.OutputEvent:
		return tui.OutputMsg{Chunk: m.Chunk}
	case pipeline.TauntEvent:
		return tui.TauntMsg{PhaseNumber: m.PhaseNumber}
	case pipeline.SummaryEvent:
		return tui.SummaryMsg{PhasesCompleted: m.PhasesCompleted, PhasesFailed: m.PhasesFailed, Duration: parseDuration(m.Duration), Branch: m.Branch}
	default:
		return nil
	}
}

func parseDuration(s string) time.Duration {
	d, _ := time.ParseDuration(s)
	return d
}
