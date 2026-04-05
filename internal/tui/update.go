package tui

import tea "github.com/charmbracelet/bubbletea"

// Listen returns a Bubble Tea command that waits for the next pipeline message.
func Listen(ch <-chan tea.Msg) tea.Cmd {
	return waitForEvent(ch)
}
