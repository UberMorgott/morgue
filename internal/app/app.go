package app

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/engine"
	"github.com/UberMorgott/morgue/internal/tui/components"
	"github.com/UberMorgott/morgue/internal/util"
)

// WizardStep represents the current screen in the TUI wizard.
type WizardStep int

const (
	StepInputPicker WizardStep = iota
	StepScanning
	StepTargetSelect
	StepToolCheck
	StepPipeline
	StepResults
)

var stepNames = []string{
	"Input", "Scan", "Select", "Tools", "Pipeline", "Results",
}

// Model is the top-level Bubble Tea model for the TUI.
type Model struct {
	step      WizardStep
	theme     Theme
	cfg       config.Config
	engine    *engine.Engine
	statusBar *components.StatusBar
	width     int
	height    int
	quitting  bool

	// Per-step state (will be populated in later tasks)
	message string // placeholder for current step content
}

// New creates a new TUI app model.
func New() Model {
	th := CyberpunkTheme()
	cfg, _ := config.Load(util.ConfigPath())
	eng := engine.New(cfg, util.BaseDir())

	sb := components.NewStatusBar(th.BG, th.FG)
	sb.SetLeft("MORGUE")
	sb.SetRight("q: quit")

	return Model{
		step:      StepInputPicker,
		theme:     th,
		cfg:       cfg,
		engine:    eng,
		statusBar: sb,
		width:     80,
		height:    24,
		message:   "Welcome to Morgue. Press enter to begin.",
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.statusBar.SetWidth(m.width)
		return m, nil

	case tea.KeyPressMsg:
		key := tea.Key(msg)
		switch {
		case key.Code == 'q' || key.Code == 'Q':
			m.quitting = true
			return m, tea.Quit
		case key.Code == rune(tea.KeyEscape):
			m.quitting = true
			return m, tea.Quit
		}
	}

	return m, nil
}

// View implements tea.Model.
func (m Model) View() tea.View {
	if m.quitting {
		return tea.NewView("Goodbye.\n")
	}

	var b strings.Builder

	// Header
	title := m.theme.Title().Render("MORGUE")
	subtitle := m.theme.Subtitle().Render("Binary Decompiler")
	b.WriteString(title + " " + subtitle + "\n\n")

	// Step indicator
	b.WriteString(m.renderStepIndicator())
	b.WriteString("\n\n")

	// Content area (placeholder)
	contentStyle := lipgloss.NewStyle().
		Width(m.width - 4).
		Padding(1, 2)
	b.WriteString(contentStyle.Render(m.message))

	// Pad to fill screen
	lines := strings.Count(b.String(), "\n")
	for i := lines; i < m.height-2; i++ {
		b.WriteString("\n")
	}

	// Status bar
	b.WriteString(m.statusBar.View())

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

func (m Model) renderStepIndicator() string {
	var parts []string
	for i, name := range stepNames {
		var icon string
		style := lipgloss.NewStyle()

		if WizardStep(i) < m.step {
			icon = "✓"
			style = style.Foreground(lipgloss.Color(m.theme.Accent))
		} else if WizardStep(i) == m.step {
			icon = "▸"
			style = style.Foreground(lipgloss.Color(m.theme.Accent)).Bold(true)
		} else {
			icon = "○"
			style = style.Foreground(lipgloss.Color(m.theme.Dim))
		}

		parts = append(parts, style.Render(fmt.Sprintf("%s %s", icon, name)))
	}

	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Dim))
	return strings.Join(parts, dimStyle.Render(" → "))
}

// RunTUI launches the TUI application.
func RunTUI() error {
	m := New()
	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}
