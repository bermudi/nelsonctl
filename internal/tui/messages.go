package tui

import (
	"time"

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

// SummaryMsg sets the final exit summary.
type SummaryMsg struct {
	PhasesCompleted int
	PhasesFailed    int
	Duration        time.Duration
	Branch          string
}

// TauntMsg is emitted when a phase exhausts all retry attempts.
type TauntMsg struct {
	PhaseNumber int
}
