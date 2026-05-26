package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// InputPickerResult holds the user's input/output directory choices.
type InputPickerResult struct {
	InputDir  string
	OutputDir string
}

// InputPicker is a huh form for selecting input/output directories.
type InputPicker struct {
	form      *huh.Form
	inputDir  string
	outputDir string
	done      bool
}

// NewInputPicker creates a new input picker form.
func NewInputPicker(defaultOutput string) *InputPicker {
	ip := &InputPicker{
		outputDir: defaultOutput,
	}

	ip.form = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Target directory or file").
				Placeholder("/path/to/binary").
				Value(&ip.inputDir),
			huh.NewInput().
				Title("Output directory").
				Placeholder(defaultOutput).
				Value(&ip.outputDir),
		),
	)

	return ip
}

// Init initializes the form.
func (ip *InputPicker) Init() tea.Cmd {
	return ip.form.Init()
}

// Update handles form events.
func (ip *InputPicker) Update(msg tea.Msg) tea.Cmd {
	form, cmd := ip.form.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		ip.form = f
	}

	if ip.form.State == huh.StateCompleted {
		ip.done = true
	}

	return cmd
}

// View renders the form.
func (ip *InputPicker) View() string {
	return ip.form.View()
}

// Done returns true when the form is completed.
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
