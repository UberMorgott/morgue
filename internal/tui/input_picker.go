package tui

import (
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/filepicker"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
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

	return &InputPicker{
		fp:            fp,
		phase:         phasePickInput,
		defaultOutput: defaultOutput,
	}
}

// Init initializes the filepicker.
func (ip *InputPicker) Init() tea.Cmd {
	return ip.fp.Init()
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
