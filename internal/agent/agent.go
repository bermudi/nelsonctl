package agent

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Agent executes a prompt against an AI CLI.
type Agent interface {
	Name() string
	Available() error
	Run(ctx context.Context, prompt string, workDir string) (*Result, error)
}

// Result captures the output of an agent invocation.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}

// StreamCallback receives stdout chunks as they are produced.
type StreamCallback func(chunk []byte)

type commandRunner func(
	ctx context.Context,
	binary string,
	args []string,
	workDir string,
	timeout time.Duration,
	terminationGrace time.Duration,
	stdoutCallback StreamCallback,
) (*Result, error)

type adapter struct {
	name             string
	binary           string
	buildArgs        func(prompt string) []string
	timeout          time.Duration
	terminationGrace time.Duration
	stdoutCallback   StreamCallback
	lookupPath       func(string) (string, error)
	runner           commandRunner
}

const defaultTerminationGrace = 2 * time.Second

// Option configures an adapter.
type Option func(*adapter)

// WithTimeout sets a per-run timeout.
func WithTimeout(timeout time.Duration) Option {
	return func(a *adapter) {
		a.timeout = timeout
	}
}

// WithStdoutCallback streams stdout chunks to the provided callback.
func WithStdoutCallback(callback StreamCallback) Option {
	return func(a *adapter) {
		a.stdoutCallback = callback
	}
}

// WithTerminationGracePeriod customizes the SIGTERM-to-SIGKILL grace period.
func WithTerminationGracePeriod(grace time.Duration) Option {
	return func(a *adapter) {
		a.terminationGrace = grace
	}
}

func newAdapter(name, binary string, buildArgs func(prompt string) []string, opts ...Option) *adapter {
	a := &adapter{
		name:             name,
		binary:           binary,
		buildArgs:        buildArgs,
		terminationGrace: defaultTerminationGrace,
		lookupPath:       exec.LookPath,
		runner:           runCommand,
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Name returns the adapter name.
func (a *adapter) Name() string {
	return a.name
}

// Available checks whether the adapter binary is on PATH.
func (a *adapter) Available() error {
	if a.binary == "" {
		return errors.New("agent binary is not configured")
	}

	lookup := a.lookupPath
	if lookup == nil {
		lookup = exec.LookPath
	}

	if _, err := lookup(a.binary); err != nil {
		return fmt.Errorf("%s not available on PATH: %w", a.binary, err)
	}

	return nil
}

// Run executes the adapter with the given prompt and working directory.
func (a *adapter) Run(ctx context.Context, prompt string, workDir string) (*Result, error) {
	if a.buildArgs == nil {
		return nil, errors.New("agent command builder is not configured")
	}

	runner := a.runner
	if runner == nil {
		runner = runCommand
	}

	return runner(
		ctx,
		a.binary,
		a.buildArgs(prompt),
		workDir,
		a.timeout,
		a.terminationGrace,
		a.stdoutCallback,
	)
}

// New selects an adapter by CLI name.
func New(name string, opts ...Option) (Agent, error) {
	switch strings.ToLower(name) {
	case "opencode":
		return NewOpencode(opts...), nil
	case "claude":
		return NewClaude(opts...), nil
	case "codex":
		return NewCodex(opts...), nil
	case "amp":
		return NewAmp(opts...), nil
	default:
		return nil, fmt.Errorf("unknown agent %q", name)
	}
}

func promptArgs(prompt string) []string {
	return []string{"--prompt", prompt}
}
