package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/engine"
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

	eng := engine.New(cfg, util.BaseDir())

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	m := newWatchModel()
	p := tea.NewProgram(m, tea.WithOutput(os.Stderr))

	events := make(chan engine.PipelineEvent, 100)

	// Forward pipeline events to TUI
	go func() {
		for ev := range events {
			p.Send(tui.PipelineEventMsg{Event: ev})
		}
	}()

	// Run pipeline in background
	var pipeErr error
	go func() {
		pipeOpts := engine.Options{
			Input:   opts.Target,
			Output:  opts.Output,
			Recipe:  opts.Recipe,
			NoSkip:  opts.NoSkip,
			Exclude: opts.Exclude,
		}
		pipeErr = eng.Run(ctx, pipeOpts, events)
		p.Send(tui.PipelineDoneMsg{Err: pipeErr})
	}()

	// Run TUI (blocks until done)
	if _, err := p.Run(); err != nil {
		return err
	}

	// Output summary JSON to stdout
	summaryPath := opts.Output + "/summary.json"
	data, err := os.ReadFile(summaryPath)
	if err == nil {
		var pretty interface{}
		if json.Unmarshal(data, &pretty) == nil {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(pretty)
		}
	}

	return pipeErr
}
