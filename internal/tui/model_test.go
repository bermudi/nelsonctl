package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/bermudi/nelsonctl/internal/config"
	"github.com/bermudi/nelsonctl/internal/pipeline"
	tea "github.com/charmbracelet/bubbletea"
)

func TestModelUpdateTransitions(t *testing.T) {
	m := NewModel([]pipeline.Phase{
		{Number: 1, Name: "Foundation"},
		{Number: 2, Name: "Adapter"},
	})

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)

	updated, _ = m.Update(StateMsg{State: pipeline.StatePhaseLoop})
	m = updated.(Model)
	updated, _ = m.Update(ExecutionContextMsg{Mode: config.ModeNelson, Agent: "pi", Step: "review", Model: "moonshotai/kimi-k2.5", Resumed: true})
	m = updated.(Model)
	updated, _ = m.Update(ControllerActivityMsg{Tool: "run_review"})
	m = updated.(Model)
	updated, _ = m.Update(PhaseMsg{Number: 1, Name: "Foundation", Attempt: 1})
	m = updated.(Model)
	updated, _ = m.Update(OutputMsg{Chunk: "agent line one"})
	m = updated.(Model)
	updated, _ = m.Update(PhaseResultMsg{Number: 1, Passed: true, Attempts: 2, Review: "all good"})
	m = updated.(Model)
	updated, _ = m.Update(SummaryMsg{PhasesCompleted: 2, PhasesFailed: 0, TotalAttempts: 4, Duration: 3 * time.Minute, Branch: "change/initial-scaffold", Mode: config.ModeNelson, Resumed: true})
	m = updated.(Model)

	view := m.View()
	for _, want := range []string{"Progress", "Output", "● passed", "Retries: 1/3", "Mode: Nelson", "Agent: pi", "Model: moonshotai/kimi-k2.5", "Resume: true", "Controller: ⚙ Controller: running review...", "Phases completed: 2", "Total attempts: 4", "Duration: 3m0s", "Branch: change/initial-scaffold", "agent line one"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() missing %q in %q", want, view)
		}
	}
}

func TestModelBatchesAgentStreamOutput(t *testing.T) {
	m := NewModel(nil)
	updated, cmd := m.Update(AgentStreamMsg{Chunk: "hello ", Metadata: map[string]string{"session_id": "s1"}})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected flush command")
	}
	updated, _ = m.Update(AgentStreamMsg{Chunk: "world", Metadata: map[string]string{"session_id": "s1"}})
	m = updated.(Model)
	updated, _ = m.Update(agentFlushMsg{})
	m = updated.(Model)
	if got := strings.Join(m.outputLines, "\n"); !strings.Contains(got, "hello world") {
		t.Fatalf("output = %q", got)
	}
}

func TestControllerStatusMapping(t *testing.T) {
	if got := controllerStatus(ControllerActivityMsg{Analyzing: true}); got != "⚙ Controller: analyzing..." {
		t.Fatalf("analyzing = %q", got)
	}
	if got := controllerStatus(ControllerActivityMsg{Tool: "submit_prompt"}); got != "⚙ Controller: sending apply prompt..." {
		t.Fatalf("submit_prompt = %q", got)
	}
	if got := controllerStatus(ControllerActivityMsg{Tool: "approve", Summary: "all clear"}); got != "⚙ Controller: approved - all clear" {
		t.Fatalf("approve = %q", got)
	}
}

func TestModelKeyBindings(t *testing.T) {
	m := NewModel(nil).WithEventChannel(make(chan tea.Msg))

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(Model)
	if !m.paused {
		t.Fatal("paused = false, want true")
	}
	if cmd == nil {
		t.Fatal("expected follow-up command for pause")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = updated.(Model)
	if !m.aborted {
		t.Fatal("aborted = false, want true")
	}
}

func TestListenReturnsChannelMessage(t *testing.T) {
	ch := make(chan tea.Msg, 1)
	ch <- OutputMsg{Chunk: "hello"}

	msg := Listen(ch)()
	out, ok := msg.(OutputMsg)
	if !ok {
		t.Fatalf("Listen() returned %T, want OutputMsg", msg)
	}
	if out.Chunk != "hello" {
		t.Fatalf("OutputMsg.Chunk = %q, want hello", out.Chunk)
	}
}

func TestModelTauntMsgRendersInOutput(t *testing.T) {
	m := NewModel([]pipeline.Phase{
		{Number: 1, Name: "Foundation"},
	})

	updated, _ := m.Update(TauntMsg{PhaseNumber: 1})
	m = updated.(Model)
	if m.taunt != "HA-ha!" {
		t.Fatalf("taunt = %q, want HA-ha!", m.taunt)
	}

	view := m.View()
	if !strings.Contains(view, "HA-ha!") {
		t.Fatalf("View() missing HA-ha! in %q", view)
	}
}

func TestModelPauseSignalsChannel(t *testing.T) {
	m := NewModel(nil)
	m = m.WithEventChannel(make(chan tea.Msg))

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(Model)
	if !m.paused {
		t.Fatal("paused = false after pressing p")
	}
	select {
	case <-m.PauseChan():
	default:
		t.Fatal("pause channel should have a signal")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(Model)
	if m.paused {
		t.Fatal("paused = true after pressing p again")
	}
	select {
	case <-m.ResumeChan():
	default:
		t.Fatal("resume channel should have a signal")
	}
}
