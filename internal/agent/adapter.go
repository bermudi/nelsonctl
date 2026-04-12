package agent

import (
	"context"
	"encoding/json"
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
	StepCommit      Step = "commit"
)

type EventType string

const (
	TextEvent       EventType = "text"
	ErrorEvent      EventType = "error"
	CompletionEvent EventType = "completion"
)

type Event struct {
	Type         EventType
	Content      string
	Metadata     map[string]string
	TracePayload interface{}
}

type SessionCreatedEvent struct {
	SessionID     string `json:"session_id"`
	SessionType   string `json:"session_type"`
	ParentSession string `json:"parent_session,omitempty"`
}

type SessionSwitchedEvent struct {
	SessionID string `json:"session_id"`
}

type ModelSetEvent struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Success  bool   `json:"success"`
}

type RPCRawEvent struct {
	RPCType        string          `json:"rpc_type"`
	SessionID      string          `json:"session_id,omitempty"`
	StopReason     string          `json:"stop_reason,omitempty"`
	PayloadSummary string          `json:"payload_summary,omitempty"`
	Payload        json.RawMessage `json:"payload,omitempty"`
	DroppedCount   int             `json:"dropped_count,omitempty"`
	DroppedStream  string          `json:"dropped_stream,omitempty"`
	DroppedReason  string          `json:"dropped_reason,omitempty"`
}

type EventsDrainedEvent struct {
	Count int `json:"count"`
}

type AgentRestartedEvent struct {
	Cause string `json:"cause"`
}

type CommandStartEvent struct {
	Name            string   `json:"name"`
	Command         string   `json:"command,omitempty"`
	Args            []string `json:"args,omitempty"`
	WorkDir         string   `json:"work_dir,omitempty"`
	Source          string   `json:"source,omitempty"`
	SessionID       string   `json:"session_id,omitempty"`
	Step            string   `json:"step,omitempty"`
	ToolCallID      string   `json:"tool_call_id,omitempty"`
	ToolName        string   `json:"tool_name,omitempty"`
	ProviderSummary string   `json:"provider_summary,omitempty"`
}

type CommandResultEvent struct {
	Name        string   `json:"name"`
	Command     string   `json:"command,omitempty"`
	ExitCode    int      `json:"exit_code"`
	DurationMs  int64    `json:"duration_ms"`
	StdoutLen   int      `json:"stdout_len,omitempty"`
	StderrLen   int      `json:"stderr_len,omitempty"`
	Error       string   `json:"error,omitempty"`
	Source      string   `json:"source,omitempty"`
	SessionID   string   `json:"session_id,omitempty"`
	Step        string   `json:"step,omitempty"`
	ToolCallID  string   `json:"tool_call_id,omitempty"`
	ToolName    string   `json:"tool_name,omitempty"`
	CommandName string   `json:"command_name,omitempty"`
	CommandArgs []string `json:"command_args,omitempty"`
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
