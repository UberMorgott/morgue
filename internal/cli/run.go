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
// plain line-streaming path instead. It is a package var so tests can override
// the TTY probe and exercise the non-interactive branch deterministically.
var stderrIsTerminal = func() bool {
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
	// Cflow opts into the experimental control-flow deobfuscation pass (default off).
	Cflow bool
	// GameDataOut is the destination for the organized game-data tree
	// (unity-mono recipe). Empty falls back to <target output>/GameData.
	GameDataOut string
	// ToolsMirror overrides config.ToolsMirror for this run: rewrites tool
	// download URL hosts to an internal mirror for offline/firewalled installs.
	ToolsMirror string
	// ForceWatch runs the watch TUI even when stderr is not a TTY. Off by
	// default because the TUI draws nothing to a redirected stderr; opt in when
	// the caller knows a terminal is attached (or wants the alt-screen anyway).
	ForceWatch bool
}

// Run executes the decompilation pipeline from CLI.
func Run(opts RunOptions) error {
	if opts.Watch && !opts.Quiet {
		if stderrIsTerminal() || opts.ForceWatch {
			return RunWatch(opts)
		}
		// Non-interactive stderr (redirected/piped): the watch TUI would render
		// nothing, so degrade to the plain line-streaming path below. Say so
		// explicitly rather than silently disabling — the plain log still streams
		// per-step progress; use -q for JSON-only output, or --force-watch to keep
		// the TUI anyway.
		fmt.Fprintln(os.Stderr, "[watch] stderr is not a TTY — showing plain progress log (use -q for JSON-only output)")
	}

	cfg, err := config.Load(util.ConfigPath())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// A --tools-mirror flag overrides the configured mirror for this run.
	if opts.ToolsMirror != "" {
		cfg.ToolsMirror = opts.ToolsMirror
	}

	if opts.Output == "" {
		opts.Output = cfg.DefaultOutputDir
	}
	if opts.Output == "" {
		// Implicit default: most-free disk (avoids C:/E:, which run out of space
		// on tens-of-GB exports). An explicit -o or configured dir still wins above.
		opts.Output = util.AutoOutputRoot()
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
			} else if ev.Severity == "warn" {
				fmt.Fprintf(os.Stderr, "[%s] WARN: %s\n", ev.Phase, ev.Message)
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
		Cflow:        opts.Cflow,
		GameDataOut:  opts.GameDataOut,
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
