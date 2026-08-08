package printstyle

import "charm.land/lipgloss/v2"

var StyleInfo = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("#7D56F4"))

var StyleSuccess = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("#33db98"))

var StyleError = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("#ff6666"))
