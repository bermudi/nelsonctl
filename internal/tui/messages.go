package tui

import (
	"time"

	"github.com/bermudi/nelsonctl/internal/config"
	"github.com/bermudi/nelsonctl/internal/pipeline"
)

// Message is a pipeline event consumed by the TUI.
type Message interface{}

// StateMsg updates the coarse pipeline stage.
type StateMsg struct {
	State pipeline.State
}

// PhaseMsg marks a phase as started/running.
type PhaseMsg struct {
	Number  int
	Name    string
	Attempt int
}

// PhaseResultMsg marks a phase as passed or failed.
type PhaseResultMsg struct {
	Number    int
	Passed    bool
	Attempts  int
	Review    string
	UpdatedAt time.Time
}

// OutputMsg appends agent output to the log pane.
type OutputMsg struct {
	Chunk string
}

// AgentStreamMsg appends streamed agent output, potentially batched by the TUI.
type AgentStreamMsg struct {
	Chunk    string
	Metadata map[string]string
}

// AgentStatusMsg appends non-streaming agent status notices.
type AgentStatusMsg struct {
	Text string
}

// ExecutionContextMsg updates the active execution context shown in the TUI.
type ExecutionContextMsg struct {
	Mode    config.ExecutionMode
	Agent   string
	Step    string
	Model   string
	Resumed bool
}

// ControllerActivityMsg reflects a controller tool call mechanically.
type ControllerActivityMsg struct {
	Tool      string
	Summary   string
	Analyzing bool
}

// SummaryMsg sets the final exit summary.
type SummaryMsg struct {
	PhasesCompleted int
	PhasesFailed    int
	TotalAttempts   int
	Duration        time.Duration
	Branch          string
	Mode            config.ExecutionMode
	Resumed         bool
}

// TauntMsg is emitted when a phase exhausts all retry attempts.
type TauntMsg struct {
	PhaseNumber int
}
