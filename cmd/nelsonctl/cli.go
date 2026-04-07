package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bermudi/nelsonctl/internal/agent"
	"github.com/bermudi/nelsonctl/internal/config"
	"github.com/bermudi/nelsonctl/internal/git"
	"github.com/bermudi/nelsonctl/internal/pipeline"
	"github.com/bermudi/nelsonctl/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

type options struct {
	agentName string
	dryRun    bool
	noPR      bool
	verbose   bool
}

// runCLI parses flags and executes nelsonctl.
func runCLI(ctx context.Context, args []string, cwd string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "init" {
		return runInit(stdout, stderr, stdin)
	}

	opts, changePath, err := parseArgs(args, stderr)
	if err != nil {
		return 2
	}

	cfg, _, err := config.Load()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	resolvedAgent := config.ResolveAgent(cfg, opts.agentName)
	if err := config.ValidateWorkspace(cwd); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := config.ValidateControllerCredentials(cfg, os.Getenv); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	absChangePath := changePath
	if !filepath.IsAbs(absChangePath) {
		absChangePath = filepath.Join(cwd, absChangePath)
	}

	if opts.dryRun {
		if err := printDryRun(stdout, changePath, absChangePath, cfg, resolvedAgent); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}

	agentOptions := []agent.Option{agent.WithTimeout(cfg.EffectiveTimeout())}
	if opts.verbose {
		agentOptions = append(agentOptions, agent.WithStdoutCallback(func(chunk []byte) {
			_, _ = stdout.Write(chunk)
		}))
	}

	agentClient, err := agent.New(resolvedAgent.Name, agentOptions...)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := agentClient.CheckPrerequisites(ctx); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	gitClient := git.NewClient(cwd)
	p := pipeline.New(absChangePath, cwd, agentClient, gitClient)

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
	fs.StringVar(&opts.agentName, "agent", "", "agent to use (defaults to config, pi-first)")
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

func printDryRun(stdout io.Writer, changeArg, absChangePath string, cfg config.Config, resolved config.ResolvedAgent) error {
	phases, err := pipeline.ParseTasksFile(filepath.Join(absChangePath, "tasks.md"))
	if err != nil {
		return err
	}

	branch := "change/" + filepath.Base(filepath.Clean(absChangePath))
	fmt.Fprintf(stdout, "Dry run for %s\n", changeArg)
	fmt.Fprintf(stdout, "Mode: %s\n", resolved.Mode)
	fmt.Fprintf(stdout, "Agent: %s\n", resolved.Name)
	fmt.Fprintf(stdout, "Apply model: %s\n", cfg.Steps.Apply.Model)
	fmt.Fprintf(stdout, "Review model: %s\n", cfg.Steps.Review.Model)
	fmt.Fprintf(stdout, "Fix model: %s\n", cfg.Steps.Fix.Model)
	fmt.Fprintf(stdout, "Review fail_on: %s\n", cfg.Review.FailOn)
	fmt.Fprintf(stdout, "Branch: %s\n", branch)
	for _, phase := range phases {
		if phaseDone(phase) {
			continue
		}
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

func phaseDone(phase pipeline.Phase) bool {
	if len(phase.Tasks) == 0 {
		return false
	}
	for _, task := range phase.Tasks {
		if !task.Done {
			return false
		}
	}
	return true
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
