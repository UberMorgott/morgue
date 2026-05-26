package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
)

// TargetItem represents a file in the target selection list.
type TargetItem struct {
	Path     string
	Selected bool
	Skipped  bool
	SkipCat  string
}

// TargetSelect is a checkbox list for selecting targets.
type TargetSelect struct {
	items  []TargetItem
	cursor int
	done   bool
	accent string
	dim    string
}

// NewTargetSelect creates a new target selection screen.
func NewTargetSelect(items []TargetItem, accent, dim string) *TargetSelect {
	return &TargetSelect{
		items:  items,
		accent: accent,
		dim:    dim,
	}
}

// Init returns nil.
func (ts *TargetSelect) Init() tea.Cmd { return nil }

// Update handles keyboard input.
func (ts *TargetSelect) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		key := tea.Key(msg)
		switch {
		case key.Code == tea.KeyUp || key.Code == 'k':
			if ts.cursor > 0 {
				ts.cursor--
			}
		case key.Code == tea.KeyDown || key.Code == 'j':
			if ts.cursor < len(ts.items)-1 {
				ts.cursor++
			}
		case key.Code == ' ':
			if !ts.items[ts.cursor].Skipped {
				ts.items[ts.cursor].Selected = !ts.items[ts.cursor].Selected
			}
		case key.Code == 'a':
			for i := range ts.items {
				if !ts.items[i].Skipped {
					ts.items[i].Selected = true
				}
			}
		case key.Code == 'n':
			for i := range ts.items {
				ts.items[i].Selected = false
			}
		case key.Code == tea.KeyEnter:
			ts.done = true
		}
	}
	return nil
}

// View renders the target selection list.
func (ts *TargetSelect) View() string {
	accentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ts.accent))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ts.dim))

	var b strings.Builder
	b.WriteString(accentStyle.Render("Select targets to process") + "\n")
	b.WriteString(dimStyle.Render("space: toggle  a: all  n: none  enter: continue") + "\n\n")

	for i, item := range ts.items {
		cursor := "  "
		if i == ts.cursor {
			cursor = accentStyle.Render("▸ ")
		}

		var checkbox string
		if item.Skipped {
			checkbox = dimStyle.Render("[skip:" + item.SkipCat + "]")
		} else if item.Selected {
			checkbox = accentStyle.Render("[✓]")
		} else {
			checkbox = "[ ]"
		}

		name := filepath.Base(item.Path)
		if item.Skipped {
			name = dimStyle.Render(name)
		}

		b.WriteString(fmt.Sprintf("%s%s %s\n", cursor, checkbox, name))
	}

	return b.String()
}

// Done returns true when selection is confirmed.
func (ts *TargetSelect) Done() bool { return ts.done }

// Selected returns the paths of selected items.
func (ts *TargetSelect) Selected() []string {
	var paths []string
	for _, item := range ts.items {
		if item.Selected && !item.Skipped {
			paths = append(paths, item.Path)
		}
	}
	return paths
}
