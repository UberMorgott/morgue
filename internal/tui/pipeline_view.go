package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/UberMorgott/morgue/internal/engine"
	"github.com/UberMorgott/morgue/internal/tui/components"
)

// PipelineEventMsg wraps a pipeline event for Bubble Tea.
type PipelineEventMsg struct {
	Event engine.PipelineEvent
}

// PipelineDoneMsg signals the pipeline is complete.
type PipelineDoneMsg struct {
	Err error
}

// PipelineView renders pipeline progress with a log view.
type PipelineView struct {
	progress *components.MultiProgress
	logView  *components.LogView
	start    time.Time
	done     bool
	err      error
}

// NewPipelineView creates a pipeline view with theme colors.
func NewPipelineView(accent, errColor, dim, border string) *PipelineView {
	return &PipelineView{
		progress: components.NewMultiProgress(accent, errColor, dim),
		logView:  components.NewLogView(border, 200),
		start:    time.Now(),
	}
}

// SetSteps initializes the step names.
func (pv *PipelineView) SetSteps(names []string) {
	pv.progress.SetSteps(names)
}

// SetSize sets the view dimensions.
func (pv *PipelineView) SetSize(w, h int) {
	pv.progress.SetWidth(w)
	pv.logView.SetSize(w, h/3)
}

// Init returns nil.
func (pv *PipelineView) Init() tea.Cmd { return nil }

// Update handles pipeline events.
func (pv *PipelineView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case PipelineEventMsg:
		ev := msg.Event
		if ev.Progress != nil {
			p := ev.Progress
			var state components.StepState
			switch {
			case p.Status.String() == "Running":
				state = components.StepRunning
			case p.Status.String() == "Success":
				state = components.StepDone
			case p.Status.String() == "Failed":
				state = components.StepFailed
			case p.Status.String() == "Skipped":
				state = components.StepSkipped
			default:
				state = components.StepPending
			}
			pv.progress.UpdateStep(p.Step, state, p.Duration)
		}
		if ev.Message != "" {
			pv.logView.AddLine(fmt.Sprintf("[%s] %s", ev.Phase, ev.Message))
		}
		if ev.Error != nil {
			pv.logView.AddLine(fmt.Sprintf("[ERROR] %v", ev.Error))
		}
	case PipelineDoneMsg:
		pv.done = true
		pv.err = msg.Err
	}
	return nil
}

// View renders the pipeline view.
func (pv *PipelineView) View() string {
	var b strings.Builder

	elapsed := time.Since(pv.start).Truncate(time.Second)
	b.WriteString(fmt.Sprintf("Elapsed: %s\n\n", elapsed))

	b.WriteString(pv.progress.View())
	b.WriteString("\n")
	b.WriteString(pv.logView.View())

	if pv.done {
		if pv.err != nil {
			b.WriteString(fmt.Sprintf("\n\nPipeline failed: %v", pv.err))
		} else {
			b.WriteString("\n\nPipeline complete!")
		}
	}

	return b.String()
}

// Done returns true when the pipeline is complete.
func (pv *PipelineView) Done() bool { return pv.done }
