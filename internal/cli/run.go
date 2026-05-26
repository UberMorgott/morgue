package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/engine"
	_ "github.com/UberMorgott/morgue/internal/recipe"
	"github.com/UberMorgott/morgue/internal/util"
)

// RunOptions holds CLI run command options.
type RunOptions struct {
	Target  string
	Output  string
	Recipe  string
	NoSkip  bool
	Exclude []string
	Watch   bool
}

// Run executes the decompilation pipeline from CLI.
func Run(opts RunOptions) error {
	if opts.Watch {
		return RunWatch(opts)
	}

	cfg, err := config.Load(util.ConfigPath())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	eng := engine.New(cfg, util.BaseDir())

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	events := make(chan engine.PipelineEvent, 100)

	go func() {
		for ev := range events {
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
	}

	if err := eng.Run(ctx, pipeOpts, events); err != nil {
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

	return nil
}
