package components

import (
	"strings"

	lipgloss "charm.land/lipgloss/v2"
)

// Header renders a persistent top bar with app name, version, update status, and settings.
type Header struct {
	width        int
	version      string
	updateStatus string // "checking...", "up to date", "update: vX.Y.Z", "offline"
}

// NewHeader creates a header component.
func NewHeader(version string) *Header {
	return &Header{
		width:        80,
		version:      version,
		updateStatus: "checking...",
	}
}

// SetWidth sets the header width.
func (h *Header) SetWidth(w int) {
	h.width = w
}

// SetUpdateStatus sets the update status text.
func (h *Header) SetUpdateStatus(s string) {
	h.updateStatus = s
}

// View renders the header bar.
func (h *Header) View() string {
	nameStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#bf5fff"))
	versionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#505060"))
	settingsStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#505060"))

	left := nameStyle.Render("MORGUE") + " " + versionStyle.Render(h.version)

	// Update status with appropriate color
	var statusStyled string
	switch {
	case h.updateStatus == "checking...":
		s := lipgloss.NewStyle().Foreground(lipgloss.Color("#00bfff"))
		statusStyled = s.Render("⚡ checking...")
	case h.updateStatus == "up to date":
		s := lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff9f"))
		statusStyled = s.Render("✓ up to date")
	case h.updateStatus == "offline":
		s := lipgloss.NewStyle().Foreground(lipgloss.Color("#505060"))
		statusStyled = s.Render("✗ offline")
	case strings.HasPrefix(h.updateStatus, "update:"):
		s := lipgloss.NewStyle().Foreground(lipgloss.Color("#ffb833"))
		statusStyled = s.Render("⬆ " + h.updateStatus)
	default:
		s := lipgloss.NewStyle().Foreground(lipgloss.Color("#505060"))
		statusStyled = s.Render(h.updateStatus)
	}

	settings := settingsStyle.Render("⚙ Settings")
	right := statusStyled + "  |  " + settings

	// Calculate spacing
	leftLen := lipgloss.Width(left)
	rightLen := lipgloss.Width(right)
	spacerLen := h.width - leftLen - rightLen
	if spacerLen < 1 {
		spacerLen = 1
	}
	spacer := strings.Repeat(" ", spacerLen)

	return left + spacer + right
}
