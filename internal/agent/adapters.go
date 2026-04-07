package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

type cliAdapter struct {
	name       string
	binary     string
	buildArgs  func(step Step, prompt, model string, format string) []string
	settings   settings
	lookupPath func(string) (string, error)
	runner     commandRunner
	events     chan Event
	closeOnce  sync.Once
}

func newCLIAdapter(name, binary string, buildArgs func(step Step, prompt, model string, format string) []string, opts ...Option) *cliAdapter {
	settings := defaultSettings()
	for _, opt := range opts {
		opt(&settings)
	}
	return &cliAdapter{
		name:       name,
		binary:     binary,
		buildArgs:  buildArgs,
		settings:   settings,
		lookupPath: exec.LookPath,
		runner:     runCommand,
		events:     make(chan Event, settings.eventBufferSize()),
	}
}

func (a *cliAdapter) Name() string {
	return a.name
}

func (a *cliAdapter) CheckPrerequisites(ctx context.Context) error {
	return checkBinaryPrerequisites(ctx, a.binary, a.lookupPath)
}

func (a *cliAdapter) ExecuteStep(ctx context.Context, step Step, prompt, model string) (*Result, error) {
	if a.buildArgs == nil {
		return nil, errors.New("agent command builder is not configured")
	}
	runner := a.runner
	if runner == nil {
		runner = runCommand
	}
	res, err := runner(
		ctx,
		a.binary,
		a.buildArgs(step, prompt, model, a.settings.format),
		a.settings.workDirectory(),
		a.settings.timeoutFor(step),
		a.settings.terminationGrace,
		a.wrapStdoutCallback(),
	)
	if err != nil {
		a.emit(Event{Type: ErrorEvent, Content: err.Error(), Metadata: map[string]string{"step": string(step)}})
		return res, err
	}
	if res != nil {
		a.emit(Event{Type: CompletionEvent, Content: "step completed", Metadata: map[string]string{"step": string(step), "exit_code": intString(res.ExitCode)}})
	}
	return res, nil
}

func (a *cliAdapter) Cleanup(ctx context.Context) error {
	_ = ctx
	a.closeOnce.Do(func() {
		close(a.events)
	})
	return nil
}

func (a *cliAdapter) AsRPC() RPCAgent {
	return nil
}

func (a *cliAdapter) Events() <-chan Event {
	return a.events
}

func (a *cliAdapter) wrapStdoutCallback() StreamCallback {
	return func(chunk []byte) {
		if a.settings.stdoutCallback != nil {
			a.settings.stdoutCallback(chunk)
		}
		trimmed := string(chunk)
		if trimmed != "" {
			a.emit(Event{Type: TextEvent, Content: trimmed})
		}
	}
}

func (a *cliAdapter) emit(event Event) {
	select {
	case a.events <- event:
	default:
	}
}

type jsonlCallback struct {
	wrapped StreamCallback
	buf     strings.Builder
}

func (c *jsonlCallback) onChunk(chunk []byte) {
	c.buf.Write(chunk)
	for {
		s := c.buf.String()
		idx := strings.Index(s, "\n")
		if idx < 0 {
			if len(s) > 10000 {
				c.buf.Reset()
			}
			break
		}
		line := s[:idx]
		c.buf.Reset()
		c.buf.WriteString(s[idx+1:])
		c.parseLine(line)
	}
}

func (c *jsonlCallback) parseLine(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}

	evt := struct {
		Type string          `json:"type"`
		Part json.RawMessage `json:"part"`
	}{}

	if err := json.Unmarshal([]byte(line), &evt); err != nil {
		c.wrapped([]byte(line + "\n"))
		return
	}

	switch evt.Type {
	case "text":
		var part struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(evt.Part, &part) == nil && part.Text != "" {
			c.wrapped([]byte(part.Text + "\n"))
		}
	case "tool_use":
		var part struct {
			Title string `json:"title"`
			State struct {
				Input struct {
					Command string `json:"command"`
				} `json:"input"`
			} `json:"state"`
		}
		if json.Unmarshal(evt.Part, &part) == nil {
			if part.Title != "" {
				c.wrapped([]byte("[tool] " + part.Title + "\n"))
			}
			if part.State.Input.Command != "" {
				c.wrapped([]byte("[command] " + part.State.Input.Command + "\n"))
			}
		}
	case "step_start":
		var part struct {
			Title string `json:"title"`
		}
		if json.Unmarshal(evt.Part, &part) == nil && part.Title != "" {
			c.wrapped([]byte("--- " + part.Title + " ---\n"))
		}
	case "step_finish":
		c.wrapped([]byte("[done]\n"))
	case "error":
		var part struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(evt.Part, &part) == nil && part.Message != "" {
			c.wrapped([]byte("[error] " + part.Message + "\n"))
		}
	}
}

// NewPi creates a pi adapter.
func NewPi(opts ...Option) Agent {
	return NewPiRPC(opts...)
}

// NewOpencode creates an opencode adapter.
func NewOpencode(opts ...Option) Agent {
	return newCLIAdapter("opencode", "opencode", func(step Step, prompt, model string, format string) []string {
		if format == "" {
			format = "json"
		}
		args := []string{"run", "--format", format}
		if model != "" {
			args = append(args, "--model", model)
		}
		args = append(args, prompt)
		return args
	}, opts...)
}

// NewClaude creates a claude adapter.
func NewClaude(opts ...Option) Agent {
	return newCLIAdapter("claude", "claude", func(step Step, prompt, model string, format string) []string {
		args := []string{"-p", prompt, "--allowedTools", "Bash,Read,Edit", "--output-format", "json"}
		if model != "" {
			args = append(args, "--model", model)
		}
		return args
	}, opts...)
}

// NewCodex creates a codex adapter.
func NewCodex(opts ...Option) Agent {
	return newCLIAdapter("codex", "codex", func(step Step, prompt, model string, format string) []string {
		args := []string{"exec", "--json"}
		if model != "" {
			args = append(args, "--model", model)
		}
		args = append(args, prompt)
		return args
	}, opts...)
}

// NewAmp creates an amp adapter.
func NewAmp(opts ...Option) Agent {
	return newCLIAdapter("amp", "amp", func(step Step, prompt, model string, format string) []string {
		args := []string{"--execute", "--stream-json"}
		if model != "" {
			args = append(args, "--model", model)
		}
		args = append(args, prompt)
		return args
	}, opts...)
}

func init() {
	Register("pi", NewPi)
	Register("opencode", NewOpencode)
	Register("claude", NewClaude)
	Register("codex", NewCodex)
	Register("amp", NewAmp)
}

func intString(v int) string {
	return strconv.Itoa(v)
}
