package tui

import "github.com/charmbracelet/lipgloss"

var (
	appStyle = lipgloss.NewStyle().Padding(1, 2)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("63")).
			Padding(0, 1)

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("230"))

	pendingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	runningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	passedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	failedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	mutedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	pausedStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("220"))
	tauntStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
)
