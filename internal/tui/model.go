package tui

import (
	"strings"

	"github.com/bermudi/nelsonctl/internal/pipeline"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

type PhaseView struct {
	Number  int
	Name    string
	Status  PhaseStatus
	Attempt int
	Review  string
}

type PhaseStatus string

const (
	PhasePending PhaseStatus = "pending"
	PhaseRunning PhaseStatus = "running"
	PhasePassed  PhaseStatus = "passed"
	PhaseFailed  PhaseStatus = "failed"
)

type Summary struct {
	PhasesCompleted int
	PhasesFailed    int
	Duration        string
	Branch          string
}

// Model renders pipeline state and streaming agent output.
type Model struct {
	phases         []PhaseView
	state          pipeline.State
	width          int
	height         int
	viewport       viewport.Model
	outputLines    []string
	currentPhase   int
	currentAttempt int
	maxAttempts    int
	paused         bool
	aborted        bool
	summary        *Summary
	events         <-chan tea.Msg
}

// NewModel creates a TUI model from parsed phases.
func NewModel(phases []pipeline.Phase) Model {
	views := make([]PhaseView, len(phases))
	for i, phase := range phases {
		views[i] = PhaseView{Number: phase.Number, Name: phase.Name, Status: PhasePending}
	}

	vp := viewport.New(0, 0)

	return Model{
		phases:      views,
		state:       pipeline.StateInit,
		viewport:    vp,
		maxAttempts: 3,
	}
}

// WithEventChannel wires the model to a stream of pipeline messages.
func (m Model) WithEventChannel(ch <-chan tea.Msg) Model {
	m.events = ch
	return m
}

// Init starts the message pump when a channel is configured.
func (m Model) Init() tea.Cmd {
	if m.events == nil {
		return nil
	}
	return waitForEvent(m.events)
}

// Update handles Bubble Tea messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeViewport()
		return m, m.nextEventCmd()
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.aborted = true
			return m, tea.Quit
		case "p":
			m.paused = !m.paused
			return m, m.nextEventCmd()
		case "j", "down":
			m.viewport.ScrollDown(1)
			return m, m.nextEventCmd()
		case "k", "up":
			m.viewport.ScrollUp(1)
			return m, m.nextEventCmd()
		}
	case StateMsg:
		m.state = msg.State
	case PhaseMsg:
		m.setPhaseStatus(msg.Number, PhaseRunning, msg.Attempt, "")
		m.currentPhase = msg.Number
		m.currentAttempt = msg.Attempt
	case PhaseResultMsg:
		status := PhaseFailed
		if msg.Passed {
			status = PhasePassed
		}
		m.setPhaseStatus(msg.Number, status, msg.Attempts, msg.Review)
	case OutputMsg:
		m.outputLines = append(m.outputLines, msg.Chunk)
		m.viewport.SetContent(strings.Join(m.outputLines, "\n"))
		m.viewport.GotoBottom()
	case SummaryMsg:
		m.summary = &Summary{
			PhasesCompleted: msg.PhasesCompleted,
			PhasesFailed:    msg.PhasesFailed,
			Duration:        msg.Duration.String(),
			Branch:          msg.Branch,
		}
	}

	if m.viewport.Width != m.outputWidth() || m.viewport.Height != m.outputHeight() {
		m.resizeViewport()
	}

	return m, cmd
}

func (m Model) nextEventCmd() tea.Cmd {
	if m.events == nil {
		return nil
	}
	return waitForEvent(m.events)
}

func (m *Model) setPhaseStatus(number int, status PhaseStatus, attempt int, review string) {
	for i := range m.phases {
		if m.phases[i].Number != number {
			continue
		}
		m.phases[i].Status = status
		m.phases[i].Attempt = attempt
		m.phases[i].Review = review
		return
	}
}

func (m *Model) resizeViewport() {
	m.viewport.Width = m.outputWidth()
	m.viewport.Height = m.outputHeight()
	m.viewport.SetContent(strings.Join(m.outputLines, "\n"))
}

func (m Model) outputWidth() int {
	if m.width <= 0 {
		return 60
	}
	return max(30, m.width/2-4)
}

func (m Model) outputHeight() int {
	if m.height <= 0 {
		return 20
	}
	return max(8, m.height-6)
}

func waitForEvent(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
