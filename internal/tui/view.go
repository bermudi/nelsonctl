package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// View renders the current state as a two-panel layout.
func (m Model) View() string {
	progress := m.renderProgress()
	output := m.renderOutput()
	body := lipgloss.JoinHorizontal(lipgloss.Top, progress, output)
	return appStyle.Render(body)
}

func (m Model) renderProgress() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Progress"))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render(fmt.Sprintf("State: %s", m.state)))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render(fmt.Sprintf("Retries: %d/%d", m.currentAttempt, m.maxAttempts)))
	b.WriteString("\n")

	if m.summary != nil {
		b.WriteString("\n")
		b.WriteString(titleStyle.Render("Summary"))
		b.WriteString("\n")
		b.WriteString(m.summaryLine("Phases completed", m.summary.PhasesCompleted))
		b.WriteString("\n")
		b.WriteString(m.summaryLine("Phases failed", m.summary.PhasesFailed))
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render("Duration: " + m.summary.Duration))
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render("Branch: " + m.summary.Branch))
		b.WriteString("\n")
	}

	for _, phase := range m.phases {
		b.WriteString("\n")
		b.WriteString(phaseIndicator(phase.Status))
		b.WriteString(" ")
		b.WriteString(fmt.Sprintf("%d. %s", phase.Number, phase.Name))
		if phase.Attempt > 0 {
			b.WriteString(fmt.Sprintf(" (attempt %d)", phase.Attempt))
		}
		if phase.Review != "" {
			b.WriteString("\n")
			b.WriteString(mutedStyle.Render(truncateLine(phase.Review)))
		}
	}

	return panelStyle.Width(m.outputWidth()).Render(b.String())
}

func (m Model) renderOutput() string {
	content := m.viewport.View()
	if content == "" {
		content = mutedStyle.Render("Waiting for agent output...")
	}
	return panelStyle.Width(m.outputWidth()).Render(titleStyle.Render("Output") + "\n" + content)
}

func (m Model) summaryLine(label string, value int) string {
	return mutedStyle.Render(fmt.Sprintf("%s: %d", label, value))
}

func phaseIndicator(status PhaseStatus) string {
	switch status {
	case PhaseRunning:
		return runningStyle.Render("▶ running")
	case PhasePassed:
		return passedStyle.Render("● passed")
	case PhaseFailed:
		return failedStyle.Render("● failed")
	default:
		return pendingStyle.Render("○ pending")
	}
}

func truncateLine(s string) string {
	const limit = 48
	if len(s) <= limit {
		return s
	}
	if limit <= 3 {
		return s[:limit]
	}
	return s[:limit-3] + "..."
}
