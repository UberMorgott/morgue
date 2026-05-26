package app

import (
	lipgloss "charm.land/lipgloss/v2"
)

// Theme holds the cyberpunk color palette and provides styled renderers.
type Theme struct {
	BG      string
	FG      string
	Accent  string
	Accent2 string
	Err     string
	Warn    string
	Dim     string
	Running string
}

// CyberpunkTheme returns the default cyberpunk dark theme.
func CyberpunkTheme() Theme {
	return Theme{
		BG:      "#0a0a0f",
		FG:      "#c0c0c0",
		Accent:  "#00ff9f",
		Accent2: "#bf5fff",
		Err:     "#ff3366",
		Warn:    "#ffb833",
		Dim:     "#505060",
		Running: "#00bfff",
	}
}

// Title returns a bold, accent-colored style for section titles.
func (t Theme) Title() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(t.Accent)).
		Padding(0, 1)
}

// Subtitle returns a purple accent style for subtitles.
func (t Theme) Subtitle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(t.Accent2))
}

// StatusBar returns a style for the bottom status bar.
func (t Theme) StatusBar() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.FG)).
		Background(lipgloss.Color(t.BG)).
		Padding(0, 1)
}

// ProgressDone returns a style for completed progress segments.
func (t Theme) ProgressDone() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Accent))
}

// ProgressTodo returns a style for remaining progress segments.
func (t Theme) ProgressTodo() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Dim))
}

// Dimmed returns a style for de-emphasized text.
func (t Theme) Dimmed() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Dim))
}

// ErrorText returns a style for error messages.
func (t Theme) ErrorText() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(t.Err))
}

// WarningText returns a style for warning messages.
func (t Theme) WarningText() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Warn))
}

// SuccessText returns a style for success messages.
func (t Theme) SuccessText() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(t.Accent))
}
