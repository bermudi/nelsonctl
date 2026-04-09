package agent

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type Step string

const (
	StepApply       Step = "apply"
	StepReview      Step = "review"
	StepFix         Step = "fix"
	StepFinalReview Step = "final_review"
)

type EventType string

const (
	TextEvent       EventType = "text"
	ErrorEvent      EventType = "error"
	CompletionEvent EventType = "completion"
)

type Event struct {
	Type     EventType
	Content  string
	Metadata map[string]string
}

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}

type StreamCallback func(chunk []byte)

type Option func(*settings)

func WithTimeout(timeout time.Duration) Option {
	return func(s *settings) {
		s.defaultTimeout = timeout
	}
}

func WithStepTimeout(step Step, timeout time.Duration) Option {
	return func(s *settings) {
		if s.stepSettings == nil {
			s.stepSettings = map[Step]stepSettings{}
		}
		cfg := s.stepSettings[step]
		cfg.Timeout = timeout
		s.stepSettings[step] = cfg
	}
}

func WithStepModel(step Step, model string) Option {
	return func(s *settings) {
		if s.stepSettings == nil {
			s.stepSettings = map[Step]stepSettings{}
		}
		cfg := s.stepSettings[step]
		cfg.Model = strings.TrimSpace(model)
		s.stepSettings[step] = cfg
	}
}

func WithStdoutCallback(callback StreamCallback) Option {
	return func(s *settings) {
		s.stdoutCallback = callback
	}
}

func WithTerminationGracePeriod(grace time.Duration) Option {
	return func(s *settings) {
		s.terminationGrace = grace
	}
}

func WithFormat(format string) Option {
	return func(s *settings) {
		s.format = format
	}
}

func WithWorkDir(workDir string) Option {
	return func(s *settings) {
		s.workDir = strings.TrimSpace(workDir)
	}
}

func WithEventsBufferSize(size int) Option {
	return func(s *settings) {
		s.eventsBufferSize = size
	}
}

type Agent interface {
	Name() string
	CheckPrerequisites(ctx context.Context) error
	ExecuteStep(ctx context.Context, step Step, prompt, model string) (*Result, error)
	Cleanup(ctx context.Context) error
	AsRPC() RPCAgent
	Events() <-chan Event
}

type RPCAgent interface {
	Agent
	StartImplementationSession(ctx context.Context) (string, error)
	StartReviewSession(ctx context.Context) (string, error)
	SendMessage(ctx context.Context, sessionID, prompt, model string) (*Result, error)
	SessionForStep(ctx context.Context, step Step) (string, error)
	Abort(ctx context.Context, sessionID string) error
	Close() error
}

type stepSettings struct {
	Timeout time.Duration
	Model   string
}

type settings struct {
	workDir          string
	format           string
	defaultTimeout   time.Duration
	stepSettings     map[Step]stepSettings
	terminationGrace time.Duration
	stdoutCallback   StreamCallback
	eventsBufferSize int
}

func defaultSettings() settings {
	return settings{
		terminationGrace: defaultTerminationGrace,
		stepSettings:     map[Step]stepSettings{},
	}
}

const defaultTerminationGrace = 250 * time.Millisecond

func (s settings) workDirectory() string {
	if strings.TrimSpace(s.workDir) != "" {
		return s.workDir
	}
	return "."
}

func (s settings) timeoutFor(step Step) time.Duration {
	if cfg, ok := s.stepSettings[step]; ok && cfg.Timeout > 0 {
		return cfg.Timeout
	}
	return s.defaultTimeout
}

func (s settings) modelFor(step Step) string {
	if cfg, ok := s.stepSettings[step]; ok && strings.TrimSpace(cfg.Model) != "" {
		return strings.TrimSpace(cfg.Model)
	}
	return ""
}

func (s settings) eventBufferSize() int {
	if s.eventsBufferSize > 0 {
		return s.eventsBufferSize
	}
	return 128
}

type Constructor func(opts ...Option) Agent

var (
	registryMu sync.RWMutex
	registry   = map[string]Constructor{}
)

func Register(name string, constructor Constructor) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[strings.ToLower(strings.TrimSpace(name))] = constructor
}

func New(name string, opts ...Option) (Agent, error) {
	registryMu.RLock()
	constructor, ok := registry[strings.ToLower(strings.TrimSpace(name))]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown agent %q", name)
	}
	return constructor(opts...), nil
}

type commandRunner func(
	ctx context.Context,
	binary string,
	args []string,
	workDir string,
	timeout time.Duration,
	terminationGrace time.Duration,
	stdoutCallback StreamCallback,
) (*Result, error)

func checkBinaryPrerequisites(ctx context.Context, binary string, lookupPath func(string) (string, error)) error {
	if strings.TrimSpace(binary) == "" {
		return errors.New("agent binary is not configured")
	}
	if lookupPath == nil {
		lookupPath = exec.LookPath
	}
	if _, err := lookupPath(binary); err != nil {
		if binary == "pi" {
			return fmt.Errorf("pi is required for Nelson mode; install pi or choose an explicit CLI agent: %w", err)
		}
		return fmt.Errorf("%s not available on PATH: %w", binary, err)
	}
	if binary == "pi" {
		cmd := exec.CommandContext(ctx, binary, "--mode", "rpc", "--no-extensions", "--version")
		if out, err := cmd.CombinedOutput(); err != nil {
			trimmed := strings.TrimSpace(string(out))
			if trimmed == "" {
				return fmt.Errorf("pi is installed but failed the RPC startup check: %w", err)
			}
			return fmt.Errorf("pi is installed but failed the RPC startup check: %w: %s", err, trimmed)
		}
	}
	return nil
}
