package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	"github.com/UberMorgott/morgue/internal/tools"
)

// ToolDownloadMsg reports a tool download progress.
type ToolDownloadMsg struct {
	Name string
	Done bool
	Err  error
}

// ToolCheckDoneMsg signals all tool checks/installs are complete.
type ToolCheckDoneMsg struct{}

// ToolCheck displays tool installation status.
type ToolCheck struct {
	statuses []tools.ToolStatus
	done     bool
	accent   string
	err      string
	dim      string
}

// NewToolCheck creates a tool check screen.
func NewToolCheck(statuses []tools.ToolStatus, accent, errColor, dim string) *ToolCheck {
	return &ToolCheck{
		statuses: statuses,
		accent:   accent,
		err:      errColor,
		dim:      dim,
	}
}

// Init returns nil.
func (tc *ToolCheck) Init() tea.Cmd { return nil }

// Update handles messages.
func (tc *ToolCheck) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case ToolDownloadMsg:
		for i := range tc.statuses {
			if tc.statuses[i].Name == msg.Name {
				tc.statuses[i].Installed = msg.Done && msg.Err == nil
			}
		}
	case ToolCheckDoneMsg:
		tc.done = true
	case tea.KeyPressMsg:
		key := tea.Key(msg)
		if key.Code == tea.KeyEnter && tc.done {
			// signal to advance
		}
	}
	return nil
}

// View renders the tool check list.
func (tc *ToolCheck) View() string {
	accentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(tc.accent))
	errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(tc.err))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(tc.dim))

	var b strings.Builder
	b.WriteString(accentStyle.Render("Tool Status") + "\n\n")

	for _, s := range tc.statuses {
		var icon string
		if s.Installed {
			icon = accentStyle.Render("✓")
		} else {
			icon = errStyle.Render("✗")
		}
		b.WriteString(fmt.Sprintf("  %s %s\n", icon, s.Name))
	}

	if tc.done {
		b.WriteString("\n" + dimStyle.Render("Press enter to continue"))
	} else {
		b.WriteString("\n" + dimStyle.Render("Checking tools..."))
	}

	return b.String()
}

// Done returns true when all checks are complete.
func (tc *ToolCheck) Done() bool { return tc.done }

// AllInstalled returns true if all tools are installed.
func (tc *ToolCheck) AllInstalled() bool {
	for _, s := range tc.statuses {
		if !s.Installed {
			return false
		}
	}
	return true
}
