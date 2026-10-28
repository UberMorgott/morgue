package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/engine"
	_ "github.com/UberMorgott/morgue/internal/recipe"
	"github.com/UberMorgott/morgue/internal/runmodel"
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

// hasRunSummary excludes early scan/cancellation/write failures, which may have
// left an older summary in a reused output directory.
func hasRunSummary(err error) bool {
	return err == nil || errors.Is(err, engine.ErrTargetsFailed) || errors.Is(err, engine.ErrNoSuccessfulTargets)
}

// RunOptions holds CLI run command options.
type RunOptions struct {
	Target  string
	Output  string
	Recipe  string
	NoSkip  bool
	Exclude []string
	// ReferencePaths adds dependency search directories for managed decompilers.
	ReferencePaths []string
	Watch          bool
	Quiet          bool
	// AllowDynamic opts into recipe steps that execute target code.
	AllowDynamic bool
	// Cflow opts into the experimental control-flow deobfuscation pass (default off).
	Cflow bool
	// CodeOnly skips asset extraction (AssetRipper) — decompiled code only.
	CodeOnly bool
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
	// Request, when set, is the exact run request (built by RequestFromFlags so
	// omitted vs explicitly false flags are preserved) and overrides the
	// option fields above for admission.
	Request *runmodel.RunRequest
}

// request returns the run request for opts. Without an explicit Request, a
// false bool means "omitted" (config default applies), true means requested.
func (opts RunOptions) request() runmodel.RunRequest {
	if opts.Request != nil {
		return *opts.Request
	}
	set := func(v bool) *bool {
		if !v {
			return nil
		}
		return &v
	}
	return runmodel.RunRequest{
		Path:           opts.Target,
		Output:         opts.Output,
		Recipe:         opts.Recipe,
		NoSkip:         set(opts.NoSkip),
		Exclude:        opts.Exclude,
		ReferencePaths: opts.ReferencePaths,
		AllowDynamic:   set(opts.AllowDynamic),
		Cflow:          set(opts.Cflow),
		CodeOnly:       set(opts.CodeOnly),
		GameDataOut:    opts.GameDataOut,
		ToolsMirror:    opts.ToolsMirror,
	}
}

// AddRunFlags defines the run-request flags of `morgue run` on cmd. Names and
// defaults match the HTTP RunRequest fields one-to-one.
func AddRunFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringP("output", "o", "", "Output directory (default: <binary dir>/output)")
	f.String("recipe", "", "Force specific recipe")
	f.Bool("no-skip", false, "Disable auto skip-list")
	f.StringSlice("exclude", nil, "Additional exclude patterns")
	f.StringArray("ref", nil, "Dependency search directory for .NET assemblies (repeatable)")
	f.Bool("allow-dynamic", false, "Allow recipe steps that EXECUTE target code (e.g. ConfuserEx embedded-assembly extraction)")
	f.Bool("cflow", false, "Enable control-flow deobfuscation pass (experimental, off by default)")
	f.Bool("code-only", false, "Skip Unity asset extraction (AssetRipper) — decompiled code only")
	f.String("gamedata-out", "", "Destination for the organized game-data tree (unity-mono recipe; default: <output>/GameData)")
	f.String("tools-mirror", "", "Rewrite tool download URL hosts to this mirror (offline/firewalled installs)")
}

// RequestFromFlags builds the run request from cmd's flags (see AddRunFlags).
// A bool flag is set only when given on the command line (Changed), so
// `--allow-dynamic=false` overrides a true config while omission inherits it.
func RequestFromFlags(cmd *cobra.Command, target string) runmodel.RunRequest {
	f := cmd.Flags()
	str := func(name string) string { v, _ := f.GetString(name); return v }
	flag := func(name string) *bool {
		if !f.Changed(name) {
			return nil
		}
		v, _ := f.GetBool(name)
		return &v
	}
	exclude, _ := f.GetStringSlice("exclude")
	refs, _ := f.GetStringArray("ref")
	return runmodel.RunRequest{
		Path:           target,
		Output:         str("output"),
		Recipe:         str("recipe"),
		NoSkip:         flag("no-skip"),
		Exclude:        exclude,
		ReferencePaths: refs,
		AllowDynamic:   flag("allow-dynamic"),
		Cflow:          flag("cflow"),
		CodeOnly:       flag("code-only"),
		GameDataOut:    str("gamedata-out"),
		ToolsMirror:    str("tools-mirror"),
	}
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

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Admission is shared with the GUI and the API: explicit request values >
	// config > built-in defaults, resolved once by the engine.
	req := opts.request()
	run, err := engine.New(cfg, util.ToolsBaseDir()).PrepareRun(ctx, req)
	if err != nil {
		return err
	}
	opts.Output = run.Handle().Output

	events := make(chan runmodel.Event, 4096)
	printed := make(chan struct{})
	go func() {
		defer close(printed)
		r := newLineRenderer(os.Stderr)
		for ev := range events {
			if opts.Quiet {
				continue
			}
			if err := r.Render(ev); err != nil {
				fmt.Fprintf(os.Stderr, "[event] malformed %s #%d: %v\n", ev.Type, ev.Seq, err)
			}
		}
	}()

	_, runErr := run.Execute(ctx, nil, events)
	// Execute never closes events itself: close it and let the printer drain,
	// so no progress line is lost or interleaved with the summary.
	close(events)
	<-printed
	_ = run.Close()
	if !hasRunSummary(runErr) {
		return runErr
	}

	// Output summary JSON to stdout
	summaryPath := filepath.Join(opts.Output, "summary.json")
	data, err := os.ReadFile(filepath.Clean(summaryPath))
	if err == nil {
		var pretty any
		if json.Unmarshal(data, &pretty) == nil {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(pretty); err != nil {
				fmt.Fprintf(os.Stderr, "write summary to stdout: %v\n", err)
			}
		}

		var summary engine.PipelineSummary
		if json.Unmarshal(data, &summary) == nil {
			// Print human-readable summary to stderr
			if !opts.Quiet {
				fmt.Fprintf(os.Stderr, "\nPipeline complete: %d targets — %d success, %d failed, %d skipped (%s)\n",
					summary.Stats.Total, summary.Stats.Success, summary.Stats.Failed, summary.Stats.Skipped, summary.Stats.Duration)
			}
			if summary.Stats.Success == 0 {
				if runErr != nil {
					return runErr
				}
				return fmt.Errorf("nothing was decompiled in %s: %d failed, %d skipped (unsupported or unrecognised input?)",
					req.Path, summary.Stats.Failed, summary.Stats.Skipped)
			}
		}
	}

	return runErr
}
