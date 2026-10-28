package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/engine"
	"github.com/UberMorgott/morgue/internal/runmodel"
	"github.com/UberMorgott/morgue/internal/tui"
	"github.com/UberMorgott/morgue/internal/util"
)

// watchModel wraps PipelineView as a tea.Model for watch mode.
type watchModel struct {
	pipelineView *tui.PipelineView
	quitting     bool
}

func newWatchModel() watchModel {
	pv := tui.NewPipelineView("#00ff9f", "#ff3366", "#505060", "#bf5fff")
	pv.SetSize(80, 24)
	return watchModel{pipelineView: pv}
}

func (m watchModel) Init() tea.Cmd {
	return nil
}

func (m watchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.pipelineView.SetSize(msg.Width, msg.Height)
		return m, nil

	case tea.KeyPressMsg:
		key := tea.Key(msg)
		if key.Code == 'q' || key.Code == 'Q' || key.Code == rune(tea.KeyEscape) {
			m.quitting = true
			return m, tea.Quit
		}
		if key.Code == 'c' && key.Mod == tea.ModCtrl {
			m.quitting = true
			return m, tea.Quit
		}

	case tui.PipelineEventMsg:
		m.pipelineView.Update(msg)
		return m, nil

	case tui.PipelineDoneMsg:
		m.pipelineView.Update(msg)
		m.quitting = true
		return m, tea.Quit
	}

	return m, nil
}

func (m watchModel) View() tea.View {
	if m.quitting && m.pipelineView.Done() {
		v := tea.NewView(m.pipelineView.View() + "\n")
		return v
	}

	var b strings.Builder
	b.WriteString(m.pipelineView.View())

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

// RunWatch runs the pipeline with TUI progress on stderr and JSON on stdout.
func RunWatch(opts RunOptions) error {
	cfg, err := config.Load(util.ConfigPath())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Shared admission: resolve defaults, validate and reserve output once,
	// exactly like the plain CLI, HTTP API and GUI (plan C1). The TUI then
	// renders the single typed v2 event stream, adapted to the legacy event
	// shape the PipelineView still consumes.
	req := opts.request()
	run, err := engine.New(cfg, util.ToolsBaseDir()).PrepareRun(ctx, req)
	if err != nil {
		return err
	}
	opts.Output = run.Handle().Output

	m := newWatchModel()
	p := tea.NewProgram(m, tea.WithOutput(os.Stderr))

	events := make(chan runmodel.Event, 4096)

	// Forward typed v2 events to the TUI through the legacy adapter.
	forwarded := make(chan struct{})
	go func() {
		defer close(forwarded)
		for ev := range events {
			p.Send(tui.PipelineEventMsg{Event: engine.LegacyEvent(ev)})
		}
	}()

	// Run pipeline in background
	var pipeErr error
	go func() {
		_, pipeErr = run.Execute(ctx, nil, events)
		// Execute never closes the caller's channel: close it so the forwarder
		// drains, then signal the TUI that the run is terminal.
		close(events)
		<-forwarded
		_ = run.Close()
		p.Send(tui.PipelineDoneMsg{Err: pipeErr})
	}()

	// Run TUI (blocks until done)
	if _, err := p.Run(); err != nil {
		return err
	}

	// Output summary JSON to stdout
	summaryPath := filepath.Join(opts.Output, "summary.json")
	data, err := os.ReadFile(filepath.Clean(summaryPath))
	if err == nil && hasRunSummary(pipeErr) {
		var pretty any
		if json.Unmarshal(data, &pretty) == nil {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(pretty); err != nil {
				fmt.Fprintf(os.Stderr, "write summary to stdout: %v\n", err)
			}
		}

		// Print human-readable summary to stderr
		var summary engine.PipelineSummary
		if json.Unmarshal(data, &summary) == nil {
			fmt.Fprintf(os.Stderr, "\nPipeline complete: %d targets — %d success, %d failed, %d skipped (%s)\n",
				summary.Stats.Total, summary.Stats.Success, summary.Stats.Failed, summary.Stats.Skipped, summary.Stats.Duration)
		}
	}

	return pipeErr
}
