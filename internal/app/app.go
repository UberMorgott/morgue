package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/engine"
	_ "github.com/UberMorgott/morgue/internal/recipe"
	"github.com/UberMorgott/morgue/internal/scanner"
	"github.com/UberMorgott/morgue/internal/selfupdate"
	"github.com/UberMorgott/morgue/internal/tui"
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

// Internal messages
type scanDoneMsg struct {
	result scanner.ScanResult
	err    error
}

type toolCheckDoneMsg struct{}

type updateCheckMsg struct {
	status string // "up to date", "update: vX.Y.Z", "offline"
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

	// Per-step state
	inputPicker  *tui.InputPicker
	scanResult   scanner.ScanResult
	targetSelect *tui.TargetSelect
	toolCheck    *tui.ToolCheck
	pipelineView *tui.PipelineView
	resultMsg    string

	inputDir  string
	outputDir string

	// Header
	header *components.Header

	// Version info
	version       string
	updateStatus  string // "checking...", "up to date", "update: vX.Y.Z", "offline"
}

// New creates a new TUI app model.
func New(version string) Model {
	th := CyberpunkTheme()
	cfg, _ := config.Load(util.ConfigPath())
	eng := engine.New(cfg, util.BaseDir())

	sb := components.NewStatusBar(th.BG, th.FG)
	sb.SetLeft("MORGUE")
	sb.SetRight("q: quit  esc: back")

	hdr := components.NewHeader(version)

	return Model{
		step:         StepInputPicker,
		theme:        th,
		cfg:          cfg,
		engine:       eng,
		statusBar:    sb,
		header:       hdr,
		width:        80,
		height:       24,
		inputPicker:  tui.NewInputPicker("./decompiled"),
		version:      version,
		updateStatus: "checking...",
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.inputPicker.Init(), checkForUpdate(m.version))
}

// checkForUpdate returns a tea.Cmd that checks GitHub for updates.
func checkForUpdate(version string) tea.Cmd {
	return func() tea.Msg {
		status := selfupdate.CheckStatus(version)
		return updateCheckMsg{status: status}
	}
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.statusBar.SetWidth(m.width)
		m.header.SetWidth(m.width)
		if m.pipelineView != nil {
			m.pipelineView.SetSize(m.width, m.height)
		}
		return m, nil

	case updateCheckMsg:
		m.updateStatus = msg.status
		m.header.SetUpdateStatus(msg.status)
		return m, nil

	case tea.KeyPressMsg:
		key := tea.Key(msg)
		// Settings shortcut
		if key.Code == 's' || key.Code == 'S' {
			if m.step != StepInputPicker && m.step != StepPipeline {
				m.resultMsg = "Settings not yet implemented"
				// Just flash the status bar briefly
				m.statusBar.SetRight("Settings not yet implemented")
				return m, nil
			}
		}
		// Global quit only when not in a form
		if m.step != StepInputPicker {
			if key.Code == 'q' || key.Code == 'Q' || key.Code == rune(tea.KeyEscape) {
				m.quitting = true
				return m, tea.Quit
			}
		}
	}

	// Route to current step
	switch m.step {
	case StepInputPicker:
		return m.updateInputPicker(msg)
	case StepScanning:
		return m.updateScanning(msg)
	case StepTargetSelect:
		return m.updateTargetSelect(msg)
	case StepToolCheck:
		return m.updateToolCheck(msg)
	case StepPipeline:
		return m.updatePipeline(msg)
	case StepResults:
		return m.updateResults(msg)
	}

	return m, nil
}

func (m Model) updateInputPicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.inputPicker.Update(msg)
	if m.inputPicker.Done() {
		result := m.inputPicker.Result()
		m.inputDir = result.InputDir
		m.outputDir = result.OutputDir
		if m.outputDir == "" {
			m.outputDir = "./decompiled"
		}
		m.step = StepScanning
		return m, m.startScan()
	}
	return m, cmd
}

func (m Model) updateScanning(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case scanDoneMsg:
		if msg.err != nil {
			m.resultMsg = fmt.Sprintf("Scan error: %v", msg.err)
			m.step = StepResults
			return m, nil
		}
		m.scanResult = msg.result

		// Build target items
		var items []tui.TargetItem
		for _, group := range m.scanResult.Groups {
			for _, f := range group.Files {
				item := tui.TargetItem{Path: f, Selected: true}
				if skip, cat := m.engine.ShouldSkip(filepath.Base(f)); skip {
					item.Skipped = true
					item.SkipCat = cat
					item.Selected = false
				}
				items = append(items, item)
			}
		}

		m.targetSelect = tui.NewTargetSelect(items, m.theme.Accent, m.theme.Dim)
		m.step = StepTargetSelect
		return m, nil
	}
	return m, nil
}

func (m Model) updateTargetSelect(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.targetSelect.Update(msg)
	if m.targetSelect.Done() {
		// Check tools
		statuses := m.engine.ToolsManager().CheckAll()
		m.toolCheck = tui.NewToolCheck(statuses, m.theme.Accent, m.theme.Err, m.theme.Dim)
		m.step = StepToolCheck
		return m, m.startToolCheck()
	}
	return m, cmd
}

func (m Model) updateToolCheck(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.toolCheck.Update(msg)
	switch msg.(type) {
	case tea.KeyPressMsg:
		if m.toolCheck.Done() {
			m.pipelineView = tui.NewPipelineView(
				m.theme.Accent, m.theme.Err, m.theme.Dim, m.theme.Accent2,
			)
			m.pipelineView.SetSize(m.width, m.height)
			m.step = StepPipeline
			return m, m.startPipeline()
		}
	}
	return m, cmd
}

func (m Model) updatePipeline(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.pipelineView.Update(msg)
	if m.pipelineView.Done() {
		m.resultMsg = "Pipeline complete. Check output directory for results."
		m.step = StepResults
	}
	return m, cmd
}

func (m Model) updateResults(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		key := tea.Key(msg)
		if key.Code == tea.KeyEnter || key.Code == 'q' || key.Code == 'Q' {
			m.quitting = true
			return m, tea.Quit
		}
	}
	return m, nil
}

// Commands

func (m Model) startScan() tea.Cmd {
	return func() tea.Msg {
		result, err := m.engine.Scan(m.inputDir)
		return scanDoneMsg{result: result, err: err}
	}
}

func (m Model) startToolCheck() tea.Cmd {
	return func() tea.Msg {
		return tui.ToolCheckDoneMsg{}
	}
}

func (m Model) startPipeline() tea.Cmd {
	return func() tea.Msg {
		events := make(chan engine.PipelineEvent, 100)

		go func() {
			// Drain events (in real app, would send PipelineEventMsg via Program.Send)
			for range events {
			}
		}()

		opts := engine.Options{
			Input:  m.inputDir,
			Output: m.outputDir,
			NoSkip: false,
		}

		err := m.engine.Run(context.Background(), opts, events)
		return tui.PipelineDoneMsg{Err: err}
	}
}

// View implements tea.Model.
func (m Model) View() tea.View {
	if m.quitting {
		return tea.NewView("Goodbye.\n")
	}

	var b strings.Builder

	// Header bar
	b.WriteString(m.header.View())
	b.WriteString("\n\n")

	// Step indicator
	b.WriteString(m.renderStepIndicator())
	b.WriteString("\n\n")

	// Content area
	switch m.step {
	case StepInputPicker:
		b.WriteString(m.inputPicker.View())
	case StepScanning:
		b.WriteString(m.theme.Dimmed().Render("Scanning..."))
	case StepTargetSelect:
		b.WriteString(m.targetSelect.View())
	case StepToolCheck:
		b.WriteString(m.toolCheck.View())
	case StepPipeline:
		b.WriteString(m.pipelineView.View())
	case StepResults:
		accentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Accent))
		b.WriteString(accentStyle.Render(m.resultMsg))
		b.WriteString("\n\n")
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Dim))
		b.WriteString(dimStyle.Render("Press enter or q to exit"))
	}

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
func RunTUI(version string) error {
	m := New(version)
	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}
