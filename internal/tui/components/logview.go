package components

import (
	"strings"

	lipgloss "charm.land/lipgloss/v2"
)

// LogView wraps a scrolling log display.
type LogView struct {
	lines    []string
	maxLines int
	width    int
	height   int
	border   string
}

// NewLogView creates a new log view.
func NewLogView(borderColor string, maxLines int) *LogView {
	return &LogView{
		maxLines: maxLines,
		width:    60,
		height:   10,
		border:   string(borderColor),
	}
}

// AddLine appends a log line, trimming if over maxLines.
func (lv *LogView) AddLine(line string) {
	lv.lines = append(lv.lines, line)
	if len(lv.lines) > lv.maxLines {
		lv.lines = lv.lines[len(lv.lines)-lv.maxLines:]
	}
}

// SetSize sets the display size.
func (lv *LogView) SetSize(w, h int) {
	lv.width = w
	lv.height = h
}

// View renders the log view with a border.
func (lv *LogView) View() string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(lv.border)).
		Width(lv.width - 2).
		Height(lv.height)

	// Show last N lines that fit
	visible := lv.lines
	if len(visible) > lv.height {
		visible = visible[len(visible)-lv.height:]
	}

	content := strings.Join(visible, "\n")
	return style.Render(content)
}
