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
	p.PR = noopPullRequestCreator{}

	if !opts.noPR {
		p.PR = pipeline.NewGHClient()
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
