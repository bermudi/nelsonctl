package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/bermudi/nelsonctl/internal/agent"
	"github.com/bermudi/nelsonctl/internal/config"
	"github.com/bermudi/nelsonctl/internal/controller"
	"github.com/bermudi/nelsonctl/internal/git"
	"github.com/bermudi/nelsonctl/internal/pipeline"
	"github.com/bermudi/nelsonctl/internal/trace"
	"github.com/bermudi/nelsonctl/internal/tui"
	"github.com/bermudi/nelsonctl/internal/version"
	tea "github.com/charmbracelet/bubbletea"
)

type options struct {
	agentName string
	dryRun    bool
	noPR      bool
	trace     bool
	traceDir  string
	verbose   bool
}

var newController = controller.New

// runCLI parses flags and executes nelsonctl.
func runCLI(ctx context.Context, args []string, cwd string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "init" {
		return runInit(stdout, stderr, stdin)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

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
	if err := config.ValidateAgentStepConfig(cfg, resolvedAgent.Name); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
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

	agentOptions := []agent.Option{
		agent.WithTimeout(cfg.EffectiveTimeout()),
		agent.WithWorkDir(cwd),
		agent.WithStepModel(agent.StepApply, cfg.Steps.Apply.Model),
		agent.WithStepModel(agent.StepReview, cfg.Steps.Review.Model),
		agent.WithStepModel(agent.StepFinalReview, cfg.Steps.Review.Model),
		agent.WithStepModel(agent.StepFix, cfg.Steps.Fix.Model),
		agent.WithStepTimeout(agent.StepApply, cfg.Steps.Apply.Timeout.Std()),
		agent.WithStepTimeout(agent.StepReview, cfg.Steps.Review.Timeout.Std()),
		agent.WithStepTimeout(agent.StepFinalReview, cfg.Steps.Review.Timeout.Std()),
		agent.WithStepTimeout(agent.StepFix, cfg.Steps.Fix.Timeout.Std()),
	}
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
	var cleanupOnce sync.Once
	cleanupAgent := func() {
		cleanupOnce.Do(func() {
			_ = agentClient.Cleanup(context.Background())
		})
	}
	defer cleanupAgent()
	if err := agentClient.CheckPrerequisites(runCtx); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	controllerClient, err := newController(cfg)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	gitClient := git.NewClient(cwd)
	p := pipeline.New(absChangePath, cwd, agentClient, gitClient)
	p.Controller = controllerClient
	p.Config = cfg
	p.Mode = resolvedAgent.Mode
	p.AgentName = resolvedAgent.Name

	var (
		traceWriter *trace.TraceWriter
		traceState  *traceRunState
		closeTrace  func(string, error)
	)
	if opts.trace {
		traceWriter, traceState, err = newTraceWriter(absChangePath, resolvedAgent.Name, opts.traceDir)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		closeTrace = newTraceCloser(traceWriter, traceState)
	} else {
		closeTrace = func(string, error) {}
	}

	var interrupted atomic.Bool
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		sig, ok := <-sigCh
		if !ok {
			return
		}
		interrupted.Store(true)
		cancel()
		closeTrace("interrupted", fmt.Errorf("received signal %s", sig))
	}()

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
		safeEmit := func(msg tea.Msg) {
			if msg == nil {
				return
			}
			defer func() { _ = recover() }()
			select {
			case events <- msg:
			default:
			}
		}
		p.OnEvent = func(msg pipeline.Event) {
			if traceState != nil {
				traceState.observe(msg)
				traceWriter.Send(msg)
			}
			safeEmit(toTeaMsg(msg))
		}
		p.UseAgentEventOutput = true
		p.PauseChan = model.PauseChan()
		p.ResumeChan = model.ResumeChan()

		var agentEventWG sync.WaitGroup
		agentEventWG.Add(1)
		go func() {
			defer agentEventWG.Done()
			forwardAgentEvents(agentClient.Events(), safeEmit, traceWriter)
		}()

		type runResult struct {
			report *pipeline.Report
			err    error
		}
		resultCh := make(chan runResult, 1)

		go func() {
			report, runErr := p.Run(runCtx)
			resultCh <- runResult{report: report, err: runErr}
		}()

		prog := tea.NewProgram(model, tea.WithAltScreen(), tea.WithInput(stdin), tea.WithOutput(stdout))
		if _, err := prog.Run(); err != nil {
			cleanupAgent()
			agentEventWG.Wait()
			close(events)
			closeTrace("error", err)
			fmt.Fprintln(stderr, err)
			return 1
		}

		result := <-resultCh
		cleanupAgent()
		agentEventWG.Wait()
		close(events)
		closeTrace(traceStatus(result.err, interrupted.Load(), runCtx.Err()), result.err)
		if result.err != nil {
			fmt.Fprintln(stderr, result.err)
			return 1
		}
		return 0
	}

	if traceWriter != nil {
		p.OnEvent = func(msg pipeline.Event) {
			traceState.observe(msg)
			traceWriter.Send(msg)
		}
	}

	var agentEventWG sync.WaitGroup
	agentEventWG.Add(1)
	go func() {
		defer agentEventWG.Done()
		forwardAgentEvents(agentClient.Events(), nil, traceWriter)
	}()

	report, err := p.Run(runCtx)
	cleanupAgent()
	agentEventWG.Wait()
	closeTrace(traceStatus(err, interrupted.Load(), runCtx.Err()), err)
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
	fs.BoolVar(&opts.trace, "trace", true, "write a JSONL trace file")
	fs.StringVar(&opts.traceDir, "trace-dir", "", "directory for trace files")
	fs.BoolVar(&opts.verbose, "verbose", false, "show full agent output")

	if err := fs.Parse(args); err != nil {
		return options{}, "", err
	}
	if fs.NArg() != 1 {
		return options{}, "", fmt.Errorf("change path is required")
	}

	return opts, fs.Arg(0), nil
}

type traceRunState struct {
	started time.Time

	mu              sync.Mutex
	summarySeen     bool
	phasesCompleted int
	phasesFailed    int
	phaseResults    map[int]bool
}

func newTraceWriter(changePath, agentName, traceDir string) (*trace.TraceWriter, *traceRunState, error) {
	resolvedDir, err := resolveTraceDir(traceDir)
	if err != nil {
		return nil, nil, err
	}
	started := time.Now().UTC()
	hostname, err := os.Hostname()
	if err != nil {
		return nil, nil, fmt.Errorf("resolve hostname: %w", err)
	}
	changeName := pipeline.ChangeNameFromPath(changePath)
	writer, err := trace.New(filepath.Join(resolvedDir, traceFileName(started, changeName, agentName)), trace.RunMetaEvent{
		Version:   version.Get(),
		Agent:     agentName,
		Change:    changeName,
		Hostname:  hostname,
		StartedAt: started.Format(time.RFC3339),
	})
	if err != nil {
		return nil, nil, err
	}
	return writer, &traceRunState{started: started, phaseResults: map[int]bool{}}, nil
}

func resolveTraceDir(flagValue string) (string, error) {
	if trimmed := strings.TrimSpace(flagValue); trimmed != "" {
		if err := os.MkdirAll(trimmed, 0o700); err != nil {
			return "", fmt.Errorf("create trace directory: %w", err)
		}
		return trimmed, nil
	}
	if base := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); base != "" {
		path := filepath.Join(base, "nelsonctl", "traces")
		if err := os.MkdirAll(path, 0o700); err != nil {
			return "", fmt.Errorf("create trace directory: %w", err)
		}
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	path := filepath.Join(home, ".local", "share", "nelsonctl", "traces")
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", fmt.Errorf("create trace directory: %w", err)
	}
	return path, nil
}

func traceFileName(started time.Time, changeName, agentName string) string {
	return fmt.Sprintf("%s_%s_%s.jsonl", started.UTC().Format("2006-01-02T150405Z"), changeName, agentName)
}

func newTraceCloser(writer *trace.TraceWriter, state *traceRunState) func(string, error) {
	var once sync.Once
	return func(status string, err error) {
		if writer == nil || state == nil {
			return
		}
		once.Do(func() {
			_ = writer.Close(state.runEnd(status, err))
		})
	}
}

func (s *traceRunState) observe(event pipeline.Event) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch e := event.(type) {
	case pipeline.PhaseResultEvent:
		s.phaseResults[e.Number] = e.Passed
	case pipeline.SummaryEvent:
		s.summarySeen = true
		s.phasesCompleted = e.PhasesCompleted
		s.phasesFailed = e.PhasesFailed
	}
}

func (s *traceRunState) runEnd(status string, err error) trace.RunEndEvent {
	completed, failed := s.phaseCounts()
	runEnd := trace.RunEndEvent{
		Status:          status,
		DurationMs:      time.Since(s.started).Milliseconds(),
		PhasesCompleted: completed,
		PhasesFailed:    failed,
	}
	if err != nil && status == "error" {
		runEnd.Error = err.Error()
	}
	return runEnd
}

func (s *traceRunState) phaseCounts() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.summarySeen {
		return s.phasesCompleted, s.phasesFailed
	}
	var completed, failed int
	for _, passed := range s.phaseResults {
		if passed {
			completed++
			continue
		}
		failed++
	}
	return completed, failed
}

func traceStatus(runErr error, interrupted bool, ctxErr error) string {
	if interrupted || errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) || errors.Is(ctxErr, context.Canceled) || errors.Is(ctxErr, context.DeadlineExceeded) {
		return "interrupted"
	}
	if runErr != nil {
		return "error"
	}
	return "completed"
}

func printDryRun(stdout io.Writer, changeArg, absChangePath string, cfg config.Config, resolved config.ResolvedAgent) error {
	phases, err := pipeline.ParseTasksFile(filepath.Join(absChangePath, "tasks.md"))
	if err != nil {
		return err
	}

	branch := "change/" + filepath.Base(filepath.Clean(absChangePath))
	remaining := pipeline.RemainingPhases(phases)
	resumed := len(remaining) != len(phases)
	fmt.Fprintf(stdout, "Dry run for %s\n", changeArg)
	fmt.Fprintf(stdout, "Mode: %s\n", resolved.Mode)
	fmt.Fprintf(stdout, "Agent: %s\n", resolved.Name)
	valueLabel := titleLabel(config.CodingAgentValueLabel(resolved.Name))
	fmt.Fprintf(stdout, "Apply %s: %s\n", valueLabel, cfg.Steps.Apply.Model)
	fmt.Fprintf(stdout, "Review %s: %s\n", valueLabel, cfg.Steps.Review.Model)
	fmt.Fprintf(stdout, "Fix %s: %s\n", valueLabel, cfg.Steps.Fix.Model)
	fmt.Fprintf(stdout, "Review fail_on: %s\n", cfg.Review.FailOn)
	fmt.Fprintf(stdout, "Branch: %s\n", branch)
	fmt.Fprintf(stdout, "Resume: %t\n", resumed)
	for _, phase := range remaining {
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

func (noopPullRequestCreator) Create(ctx context.Context, repoDir, title, bodyFile string) (string, string, error) {
	return "PR creation skipped (--no-pr)", "", nil
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
	case pipeline.ExecutionContextEvent:
		return tui.ExecutionContextMsg{Mode: m.Mode, Agent: m.Agent, Step: m.Step, Model: m.Model, Resumed: m.Resumed}
	case pipeline.ControllerActivityEvent:
		return tui.ControllerActivityMsg{Tool: m.Tool, Summary: m.Summary, Analyzing: m.Analyzing}
	case pipeline.TauntEvent:
		return tui.TauntMsg{PhaseNumber: m.PhaseNumber}
	case pipeline.SummaryEvent:
		return tui.SummaryMsg{PhasesCompleted: m.PhasesCompleted, PhasesFailed: m.PhasesFailed, TotalAttempts: m.TotalAttempts, Duration: parseDuration(m.Duration), Branch: m.Branch, Mode: m.Mode, Resumed: m.Resumed}
	default:
		return nil
	}
}

func forwardAgentEvents(ch <-chan agent.Event, emitTea func(tea.Msg), traceWriter *trace.TraceWriter) {
	if ch == nil {
		return
	}
	for event := range ch {
		if traceWriter != nil {
			traceWriter.Send(event)
		}
		if emitTea != nil {
			if msg := toAgentTeaMsg(event); msg != nil {
				emitTea(msg)
			}
		}
	}
}

func toAgentTeaMsg(event agent.Event) tea.Msg {
	switch event.Type {
	case agent.TextEvent:
		return tui.AgentStreamMsg{Chunk: event.Content, Metadata: event.Metadata}
	case agent.ErrorEvent:
		return tui.AgentStatusMsg{Text: event.Content}
	case agent.CompletionEvent:
		if event.Metadata["restart"] == "true" {
			return tui.AgentStatusMsg{Text: event.Content}
		}
		return nil
	default:
		return nil
	}
}

func parseDuration(s string) time.Duration {
	d, _ := time.ParseDuration(s)
	return d
}

func titleLabel(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "Model"
	}
	if len(trimmed) == 1 {
		return strings.ToUpper(trimmed)
	}
	return strings.ToUpper(trimmed[:1]) + trimmed[1:]
}
