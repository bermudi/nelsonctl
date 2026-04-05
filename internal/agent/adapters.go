package agent

// NewOpencode creates an opencode adapter.
func NewOpencode(opts ...Option) Agent {
	return newAdapter("opencode", "opencode", func(prompt string) []string {
		return append([]string{"exec"}, promptArgs(prompt)...)
	}, opts...)
}

// NewClaude creates a claude adapter.
func NewClaude(opts ...Option) Agent {
	return newAdapter("claude", "claude", func(prompt string) []string {
		return []string{"-p", prompt}
	}, opts...)
}

// NewCodex creates a codex adapter.
func NewCodex(opts ...Option) Agent {
	return newAdapter("codex", "codex", func(prompt string) []string {
		return promptArgs(prompt)
	}, opts...)
}

// NewAmp creates an amp adapter.
func NewAmp(opts ...Option) Agent {
	return newAdapter("amp", "amp", func(prompt string) []string {
		return append([]string{"run"}, promptArgs(prompt)...)
	}, opts...)
}
