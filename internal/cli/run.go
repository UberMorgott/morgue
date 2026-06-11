package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"

	"github.com/mattn/go-isatty"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/engine"
	_ "github.com/UberMorgott/morgue/internal/recipe"
	"github.com/UberMorgott/morgue/internal/util"
)

// stderrIsTerminal reports whether stderr is an interactive terminal. The watch
// TUI renders to stderr; when stderr is redirected (e.g. a background run piping
// to a log file) the TUI draws nothing and looks hung, so we fall back to the
// plain line-streaming path instead.
func stderrIsTerminal() bool {
	fd := os.Stderr.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

// RunOptions holds CLI run command options.
type RunOptions struct {
	Target  string
	Output  string
	Recipe  string
	NoSkip  bool
	Exclude []string
	Watch   bool
	Quiet   bool
	// AllowDynamic opts into recipe steps that execute target code.
	AllowDynamic bool
}

// Run executes the decompilation pipeline from CLI.
func Run(opts RunOptions) error {
	if opts.Watch && !opts.Quiet {
		if stderrIsTerminal() {
			return RunWatch(opts)
		}
		// Non-interactive stderr (redirected/piped): the watch TUI would render
		// nothing. Degrade to the plain line-streaming path below.
		fmt.Fprintln(os.Stderr, "[watch] disabled: stderr is not a terminal, using plain line output")
	}

	cfg, err := config.Load(util.ConfigPath())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if opts.Output == "" {
		opts.Output = cfg.DefaultOutputDir
	}
	if opts.Output == "" {
		opts.Output = util.DefaultOutputDir()
	}
	os.MkdirAll(opts.Output, 0755)

	eng := engine.New(cfg, util.ToolsBaseDir())

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	events := make(chan engine.PipelineEvent, 100)

	go func() {
		for ev := range events {
			if opts.Quiet {
				continue
			}
			if ev.Progress != nil {
				p := ev.Progress
				fmt.Fprintf(os.Stderr, "[%s] Step %d/%d: %s — %s (%s)\n",
					ev.Target, p.Step+1, p.Total, p.Name, p.Status, p.Duration)
			} else if ev.Error != nil {
				fmt.Fprintf(os.Stderr, "[%s] ERROR: %v\n", ev.Phase, ev.Error)
			} else if ev.Done {
				fmt.Fprintf(os.Stderr, "[done] Pipeline complete\n")
			} else {
				fmt.Fprintf(os.Stderr, "[%s] %s\n", ev.Phase, ev.Message)
			}
		}
	}()

	pipeOpts := engine.Options{
		Input:   opts.Target,
		Output:  opts.Output,
		Recipe:  opts.Recipe,
		NoSkip:  opts.NoSkip,
		Exclude: opts.Exclude,

		AllowDynamic: opts.AllowDynamic,
	}

	if err := eng.Run(ctx, pipeOpts, events); err != nil {
		return err
	}

	// Output summary JSON to stdout
	summaryPath := opts.Output + "/summary.json"
	data, err := os.ReadFile(summaryPath)
	if err == nil {
		var pretty any
		if json.Unmarshal(data, &pretty) == nil {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(pretty)
		}

		// Print human-readable summary to stderr
		if !opts.Quiet {
			var summary engine.PipelineSummary
			if json.Unmarshal(data, &summary) == nil {
				fmt.Fprintf(os.Stderr, "\nPipeline complete: %d targets — %d success, %d failed, %d skipped (%s)\n",
					summary.Stats.Total, summary.Stats.Success, summary.Stats.Failed, summary.Stats.Skipped, summary.Stats.Duration)
			}
		}
	}

	return nil
}
