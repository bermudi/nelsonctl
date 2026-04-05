package agent

// NewOpencode creates an opencode adapter.
func NewOpencode(opts ...Option) Agent {
	return newAdapter("opencode", "opencode", func(prompt string) []string {
		return append([]string{"run", "--format", "json"}, prompt)
	}, opts...)
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
