package components

import (
	lipgloss "charm.land/lipgloss/v2"
)

// Button is a clickable rectangular TUI button.
type Button struct {
	Label      string
	X, Y       int // top-left position set by the parent layout
	Width      int // visual width including border
	Height     int // visual height including border (always 3 for single-line label)
	Focused    bool
	style      lipgloss.Style
	focusStyle lipgloss.Style
}

// NewButton creates a new button with the given label and theme colors.
func NewButton(label, accent, dim string) *Button {
	pad := 2 // horizontal padding inside border
	w := len(label) + pad*2 + 2 // +2 for left/right border chars

	base := lipgloss.NewStyle().
		Padding(0, pad).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(dim))

	focus := lipgloss.NewStyle().
		Padding(0, pad).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(accent)).
		Foreground(lipgloss.Color(accent))

	return &Button{
		Label:      label,
		Width:      w,
		Height:     3,
		style:      base,
		focusStyle: focus,
	}
}

// Render returns the styled button string.
func (b *Button) Render() string {
	if b.Focused {
		return b.focusStyle.Render(b.Label)
	}
	return b.style.Render(b.Label)
}

// Contains reports whether the screen coordinate (x, y) falls within the button bounds.
func (b *Button) Contains(x, y int) bool {
	return x >= b.X && x < b.X+b.Width &&
		y >= b.Y && y < b.Y+b.Height
}
