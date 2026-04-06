package agent

import (
	"encoding/json"
	"strings"
)

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

func wrapJSONLCallback(wrapped StreamCallback, chunk []byte) (StreamCallback, bool) {
	cb := &jsonlCallback{wrapped: wrapped}
	cb.onChunk(chunk)
	return cb.wrapped, true
}

// NewOpencode creates an opencode adapter.
func NewOpencode(opts ...Option) Agent {
	format := "json"
	for _, opt := range opts {
		var a adapter
		opt(&a)
		if a.format != "" {
			format = a.format
		}
	}

	a := newAdapter("opencode", "opencode", func(prompt string) []string {
		return append([]string{"run", "--format", format}, prompt)
	}, opts...)

	return a
}

// NewClaude creates a claude adapter.
func NewClaude(opts ...Option) Agent {
	return newAdapter("claude", "claude", func(prompt string) []string {
		return []string{"-p", prompt, "--allowedTools", "Bash,Read,Edit", "--output-format", "json"}
	}, opts...)
}

// NewCodex creates a codex adapter.
func NewCodex(opts ...Option) Agent {
	return newAdapter("codex", "codex", func(prompt string) []string {
		return []string{"exec", "--json", prompt}
	}, opts...)
}

// NewAmp creates an amp adapter.
func NewAmp(opts ...Option) Agent {
	return newAdapter("amp", "amp", func(prompt string) []string {
		return []string{"--execute", "--stream-json", prompt}
	}, opts...)
}
