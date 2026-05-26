package tui

import (
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/filepicker"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	"github.com/UberMorgott/morgue/internal/tui/components"
)

// InputPickerResult holds the user's input/output directory choices.
type InputPickerResult struct {
	InputDir  string
	OutputDir string
}

type pickerPhase int

const (
	phasePickInput pickerPhase = iota
	phasePickOutput
	phaseDone
)

// InputPicker uses bubbles filepicker for interactive file/directory selection.
type InputPicker struct {
	fp            filepicker.Model
	phase         pickerPhase
	inputDir      string
	outputDir     string
	defaultOutput string
	done          bool
	err           string

	// Clickable buttons
	selectBtn *components.Button
	backBtn   *components.Button
}

// NewInputPicker creates a new input picker with filepicker.
func NewInputPicker(defaultOutput string) *InputPicker {
	fp := filepicker.New()
	fp.SetHeight(15)

	// Start in current working directory
	cwd, _ := os.Getwd()
	fp.CurrentDirectory = cwd

	// Allow all files and directories
	fp.DirAllowed = true
	fp.FileAllowed = true
	fp.ShowHidden = false
	fp.ShowPermissions = false
	fp.ShowSize = true

	accent := "#00ff9f"
	dim := "#505060"

	return &InputPicker{
		fp:            fp,
		phase:         phasePickInput,
		defaultOutput: defaultOutput,
		selectBtn:     components.NewButton("Select", accent, dim),
		backBtn:       components.NewButton("Back", accent, dim),
	}
}

// Init initializes the filepicker.
func (ip *InputPicker) Init() tea.Cmd {
	return ip.fp.Init()
}

// HandleClick processes a mouse click and returns a tea.Cmd if the click was handled.
// Returns (handled, cmd).
func (ip *InputPicker) HandleClick(x, y int) (bool, tea.Cmd) {
	if ip.selectBtn.Contains(x, y) {
		// Simulate Enter key to confirm selection
		return true, func() tea.Msg {
			return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
		}
	}
	if ip.phase == phasePickOutput && ip.backBtn.Contains(x, y) {
		ip.phase = phasePickInput
		ip.inputDir = ""
		return true, nil
	}
	return false, nil
}

// Update handles filepicker events.
func (ip *InputPicker) Update(msg tea.Msg) tea.Cmd {
	// Handle keyboard shortcuts
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		key := tea.Key(msg)
		if key.Code == tea.KeyEscape && ip.phase == phasePickOutput {
			// Go back to input selection
			ip.phase = phasePickInput
			ip.inputDir = ""
			return nil
		}
	}

	var cmd tea.Cmd
	ip.fp, cmd = ip.fp.Update(msg)

	// Check if a file/dir was selected
	if didSelect, path := ip.fp.DidSelectFile(msg); didSelect {
		// If it's a file, use its directory
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			path = filepath.Dir(path)
		}

		switch ip.phase {
		case phasePickInput:
			ip.inputDir = path
			ip.phase = phasePickOutput

			// Reset filepicker for output selection
			ip.fp = filepicker.New()
			ip.fp.SetHeight(15)
			ip.fp.DirAllowed = true
			ip.fp.FileAllowed = false
			ip.fp.ShowHidden = false
			ip.fp.ShowSize = false
			cwd, _ := os.Getwd()
			ip.fp.CurrentDirectory = cwd
			return ip.fp.Init()

		case phasePickOutput:
			ip.outputDir = path
			ip.phase = phaseDone
			ip.done = true
			return nil
		}
	}

	return cmd
}

// View renders the filepicker with header.
func (ip *InputPicker) View() string {
	var b strings.Builder

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#bf5fff"))
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#505060"))
	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff9f"))

	switch ip.phase {
	case phasePickInput:
		b.WriteString(titleStyle.Render("Select target directory or file"))
		b.WriteString("\n")
		b.WriteString(hintStyle.Render("Navigate with arrows, enter to select"))
		b.WriteString("\n\n")

	case phasePickOutput:
		b.WriteString(titleStyle.Render("Select output directory"))
		b.WriteString("\n")
		b.WriteString(selectedStyle.Render("Input: "+ip.inputDir))
		b.WriteString("\n")
		b.WriteString(hintStyle.Render("Navigate with arrows, enter to select  |  esc: go back"))
		b.WriteString("\n\n")
	}

	b.WriteString(ip.fp.View())

	if ip.err != "" {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff3366"))
		b.WriteString("\n" + errStyle.Render(ip.err))
	}

	// Render clickable buttons at the bottom.
	// Count lines to calculate Y offset for button hit-testing.
	lines := strings.Count(b.String(), "\n") + 1
	b.WriteString("\n")

	ip.selectBtn.Y = lines
	ip.selectBtn.X = 2
	selectRendered := ip.selectBtn.Render()

	if ip.phase == phasePickOutput {
		ip.backBtn.Y = lines
		ip.backBtn.X = ip.selectBtn.X + ip.selectBtn.Width + 2
		b.WriteString("  " + selectRendered + "  " + ip.backBtn.Render())
	} else {
		b.WriteString("  " + selectRendered)
	}

	return b.String()
}

// Done returns true when both directories are selected.
func (ip *InputPicker) Done() bool {
	return ip.done
}

// Result returns the user's choices.
func (ip *InputPicker) Result() InputPickerResult {
	return InputPickerResult{
		InputDir:  ip.inputDir,
		OutputDir: ip.outputDir,
	}
}
