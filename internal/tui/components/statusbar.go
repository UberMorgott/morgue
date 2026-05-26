package components

import (
	"strings"

	lipgloss "charm.land/lipgloss/v2"
)

// StatusBar renders a left-right status bar.
type StatusBar struct {
	width int
	left  string
	right string
	bg    string
	fg    string
}

// NewStatusBar creates a status bar.
func NewStatusBar(bg, fg string) *StatusBar {
	return &StatusBar{
		width: 80,
		bg:    string(bg),
		fg:    string(fg),
	}
}

// SetWidth sets the bar width.
func (sb *StatusBar) SetWidth(w int) {
	sb.width = w
}

// SetLeft sets the left text.
func (sb *StatusBar) SetLeft(s string) {
	sb.left = s
}

// SetRight sets the right text.
func (sb *StatusBar) SetRight(s string) {
	sb.right = s
}

// View renders the status bar.
func (sb *StatusBar) View() string {
	style := lipgloss.NewStyle().
		Background(lipgloss.Color(sb.bg)).
		Foreground(lipgloss.Color(sb.fg)).
		Padding(0, 1)

	left := style.Render(sb.left)
	right := style.Render(sb.right)

	leftLen := len(sb.left) + 2 // padding
	rightLen := len(sb.right) + 2
	spacerLen := sb.width - leftLen - rightLen
	if spacerLen < 0 {
		spacerLen = 0
	}
	spacer := lipgloss.NewStyle().
		Background(lipgloss.Color(sb.bg)).
		Render(strings.Repeat(" ", spacerLen))

	return left + spacer + right
}
