package components

import (
	"fmt"
	"strings"
	"time"

	lipgloss "charm.land/lipgloss/v2"
)

// StepState represents the visual state of a step.
type StepState int

const (
	StepPending StepState = iota
	StepRunning
	StepDone
	StepFailed
	StepSkipped
)

// StepEntry holds display data for a step.
type StepEntry struct {
	Name     string
	State    StepState
	Duration time.Duration
}

// MultiProgress renders an overall progress bar plus a step list.
type MultiProgress struct {
	steps    []StepEntry
	width    int
	accent   string
	errColor string
	dimColor string
}

// NewMultiProgress creates a new MultiProgress with theme colors.
func NewMultiProgress(accent, errColor, dimColor string) *MultiProgress {
	return &MultiProgress{
		width:    60,
		accent:   string(accent),
		errColor: string(errColor),
		dimColor: string(dimColor),
	}
}

// SetSteps initializes the step names.
func (mp *MultiProgress) SetSteps(names []string) {
	mp.steps = make([]StepEntry, len(names))
	for i, n := range names {
		mp.steps[i] = StepEntry{Name: n, State: StepPending}
	}
}

// UpdateStep updates the state and duration of a step.
func (mp *MultiProgress) UpdateStep(idx int, state StepState, duration time.Duration) {
	if idx >= 0 && idx < len(mp.steps) {
		mp.steps[idx].State = state
		mp.steps[idx].Duration = duration
	}
}

// SetWidth sets the rendering width.
func (mp *MultiProgress) SetWidth(w int) {
	mp.width = w
}

func (mp *MultiProgress) percent() float64 {
	if len(mp.steps) == 0 {
		return 0
	}
	done := 0
	for _, s := range mp.steps {
		if s.State == StepDone || s.State == StepFailed || s.State == StepSkipped {
			done++
		}
	}
	return float64(done) / float64(len(mp.steps))
}

// View renders the progress bar and step list.
func (mp *MultiProgress) View() string {
	if len(mp.steps) == 0 {
		return ""
	}

	var b strings.Builder

	// Progress bar
	pct := mp.percent()
	barWidth := mp.width - 10
	if barWidth < 10 {
		barWidth = 10
	}
	filled := int(pct * float64(barWidth))
	empty := barWidth - filled

	accentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(mp.accent))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(mp.dimColor))

	bar := accentStyle.Render(strings.Repeat("█", filled)) +
		dimStyle.Render(strings.Repeat("░", empty))
	b.WriteString(fmt.Sprintf(" %s %3.0f%%\n\n", bar, pct*100))

	// Step list
	for i, s := range mp.steps {
		icon := mp.stepIcon(s.State)
		dur := ""
		if s.Duration > 0 {
			dur = fmt.Sprintf(" (%s)", s.Duration.Truncate(time.Millisecond))
		}
		b.WriteString(fmt.Sprintf("  %s %d. %s%s\n", icon, i+1, s.Name, dur))
	}

	return b.String()
}

func (mp *MultiProgress) stepIcon(state StepState) string {
	accentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(mp.accent))
	errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(mp.errColor))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(mp.dimColor))

	switch state {
	case StepRunning:
		return accentStyle.Render("▸")
	case StepDone:
		return accentStyle.Render("✓")
	case StepFailed:
		return errStyle.Render("✗")
	case StepSkipped:
		return dimStyle.Render("-")
	default:
		return dimStyle.Render("○")
	}
}
