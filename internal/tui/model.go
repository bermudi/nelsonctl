package tui

import (
	"strings"
	"time"

	"github.com/bermudi/nelsonctl/internal/config"
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
	TotalAttempts   int
	Duration        string
	Branch          string
	Mode            config.ExecutionMode
	Resumed         bool
}

type ExecutionContext struct {
	Mode    config.ExecutionMode
	Agent   string
	Step    string
	Model   string
	Resumed bool
}

// Model renders pipeline state and streaming agent output.
type Model struct {
	phases             []PhaseView
	state              pipeline.State
	width              int
	height             int
	viewport           viewport.Model
	outputLines        []string
	currentPhase       int
	currentAttempt     int
	maxAttempts        int
	paused             bool
	aborted            bool
	summary            *Summary
	execution          ExecutionContext
	controller         string
	events             <-chan tea.Msg
	taunt              string
	pendingAgentOutput strings.Builder
	pauseChan          chan struct{}
	resumeChan         chan struct{}
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
		controller:  "⚙ Controller: analyzing...",
		pauseChan:   make(chan struct{}, 1),
		resumeChan:  make(chan struct{}, 1),
	}
}

// WithEventChannel wires the model to a stream of pipeline messages.
func (m Model) WithEventChannel(ch <-chan tea.Msg) Model {
	m.events = ch
	return m
}

// PauseChan returns the channel that signals the pipeline to pause.
func (m Model) PauseChan() <-chan struct{} {
	return m.pauseChan
}

// ResumeChan returns the channel that signals the pipeline to resume.
func (m Model) ResumeChan() <-chan struct{} {
	return m.resumeChan
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
	case agentFlushMsg:
		if m.pendingAgentOutput.Len() > 0 {
			m.appendOutput(m.pendingAgentOutput.String())
			m.pendingAgentOutput.Reset()
		}
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.aborted = true
			return m, tea.Quit
		case "p":
			m.paused = !m.paused
			if m.paused {
				select {
				case m.pauseChan <- struct{}{}:
				default:
				}
			} else {
				select {
				case m.resumeChan <- struct{}{}:
				default:
				}
			}
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
		m.appendOutput(msg.Chunk)
	case AgentStreamMsg:
		m.pendingAgentOutput.WriteString(msg.Chunk)
		return m, flushAgentOutput()
	case AgentStatusMsg:
		m.appendOutput(msg.Text)
	case ExecutionContextMsg:
		m.execution = ExecutionContext{Mode: msg.Mode, Agent: msg.Agent, Step: msg.Step, Model: msg.Model, Resumed: msg.Resumed}
	case ControllerActivityMsg:
		m.controller = controllerStatus(msg)
	case SummaryMsg:
		m.summary = &Summary{
			PhasesCompleted: msg.PhasesCompleted,
			PhasesFailed:    msg.PhasesFailed,
			TotalAttempts:   msg.TotalAttempts,
			Duration:        msg.Duration.String(),
			Branch:          msg.Branch,
			Mode:            msg.Mode,
			Resumed:         msg.Resumed,
		}
	case TauntMsg:
		m.taunt = "HA-ha!"
	}

	if m.viewport.Width != m.outputWidth() || m.viewport.Height != m.outputHeight() {
		m.resizeViewport()
	}

	return m, cmd
}

func (m *Model) appendOutput(chunk string) {
	if strings.TrimSpace(chunk) == "" {
		return
	}
	m.outputLines = append(m.outputLines, chunk)
	m.viewport.SetContent(strings.Join(m.outputLines, "\n"))
	m.viewport.GotoBottom()
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

type agentFlushMsg struct{}

func flushAgentOutput() tea.Cmd {
	return tea.Tick(35*time.Millisecond, func(time.Time) tea.Msg {
		return agentFlushMsg{}
	})
}

func controllerStatus(msg ControllerActivityMsg) string {
	if msg.Analyzing {
		return "⚙ Controller: analyzing..."
	}
	switch msg.Tool {
	case "submit_prompt":
		return "⚙ Controller: sending apply prompt..."
	case "run_review":
		return "⚙ Controller: running review..."
	case "approve":
		if strings.TrimSpace(msg.Summary) == "" {
			return "⚙ Controller: approved"
		}
		return "⚙ Controller: approved - " + strings.TrimSpace(msg.Summary)
	default:
		return "⚙ Controller: analyzing..."
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
