package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/recipe"
	"github.com/UberMorgott/morgue/internal/recon"
)

const terminalRecipeName = "__engine_terminal_contract_test"

var terminalRecipeError = errors.New("terminal fixture recipe failed")

type terminalRecipe struct{}

func (terminalRecipe) Name() string             { return terminalRecipeName }
func (terminalRecipe) Description() string      { return "In-process terminal event fixture" }
func (terminalRecipe) Match(*recon.Result) bool { return false }
func (terminalRecipe) Steps() []recipe.StepInfo { return nil }
func (terminalRecipe) RequiredTools() []string  { return nil }
func (terminalRecipe) Execute(ctx *recipe.Context) error {
	if strings.HasPrefix(filepath.Base(ctx.Target), "cancel") {
		return context.Canceled
	}
	if strings.HasPrefix(filepath.Base(ctx.Target), "fail") {
		return terminalRecipeError
	}
	return os.WriteFile(filepath.Join(ctx.Output, "fixture.txt"), []byte("completed"), 0644)
}

func init() { recipe.Register(terminalRecipe{}) }

func TestRunTerminalScanFailure(t *testing.T) {
	opts := Options{Input: filepath.Join(t.TempDir(), "missing"), Output: t.TempDir()}
	events, err := runTerminalFixture(t, context.Background(), opts)
	if err == nil {
		t.Error("missing input must return an error")
	}
	assertTerminalContract(t, events, "error", nil)
	for _, event := range events {
		if event.Phase == "scan" && event.Error != nil {
			return
		}
	}
	t.Error("missing input must emit a scan error diagnostic")
}

func TestRunTerminalCancelledBeforeWork(t *testing.T) {
	input := terminalInput(t, "ok.dll")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	events, err := runTerminalFixture(t, ctx, Options{Input: input, Output: t.TempDir(), Recipe: terminalRecipeName})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run() error = %v, want context.Canceled", err)
	}
	assertTerminalContract(t, events, "cancelled", nil)
}

func TestRunTerminalWithoutSuccessfulTargets(t *testing.T) {
	for _, tc := range []struct {
		name string
		file string
	}{
		{"empty directory", ""},
		{"all targets skipped", "mscorlib.dll"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := terminalInput(t, tc.file)
			events, err := runTerminalFixture(t, context.Background(), Options{Input: input, Output: t.TempDir()})
			if err == nil {
				t.Error("run without a successful target must return an error")
			}
			wantTargets := map[string]string{}
			if tc.file != "" {
				wantTargets[filepath.Join(input, tc.file)] = "skipped"
			}
			assertTerminalContract(t, events, "error", wantTargets)
		})
	}
}

func TestRunTerminalTargetAggregation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		files   []string
		outcome string
	}{
		{"success", []string{"ok.dll"}, "success"},
		{"failure", []string{"fail.dll"}, "error"},
		{"mixed", []string{"ok.dll", "fail.dll"}, "error"},
		{"cancelled target", []string{"cancel.dll"}, "cancelled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := terminalInput(t, tc.files...)
			events, err := runTerminalFixture(t, context.Background(), Options{Input: input, Output: t.TempDir(), Recipe: terminalRecipeName})
			if (err != nil) != (tc.outcome != "success") {
				t.Errorf("Run() error = %v, want run outcome %s", err, tc.outcome)
			}
			if tc.outcome == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Errorf("cancellation cause lost from Run() error: %v", err)
			}
			wantTargets := map[string]string{}
			for _, name := range tc.files {
				outcome := "success"
				if strings.HasPrefix(name, "fail") {
					outcome = "error"
				} else if strings.HasPrefix(name, "cancel") {
					outcome = "cancelled"
				}
				wantTargets[filepath.Join(input, name)] = outcome
			}
			assertTerminalContract(t, events, tc.outcome, wantTargets)
			for _, event := range events {
				if event.TargetOutcome == "error" && !errors.Is(event.Error, terminalRecipeError) {
					t.Errorf("target error cause lost: %v", event.Error)
				}
			}
		})
	}
}

func TestRunTerminalNestedRunDoesNotFinishParent(t *testing.T) {
	input := terminalInput(t, "ok.dll")
	events, err := runTerminalFixture(t, context.Background(), Options{Input: input, Output: t.TempDir(), Recipe: terminalRecipeName, Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Done || event.RunOutcome != "" {
			t.Errorf("nested run emitted parent terminal event: %+v", event)
		}
	}
}

func terminalInput(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		if name == "" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte("inert fixture, no target execution"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func runTerminalFixture(t *testing.T, ctx context.Context, opts Options) ([]PipelineEvent, error) {
	t.Helper()
	engine := New(config.Default(), t.TempDir())
	ch := make(chan PipelineEvent, 32)
	collected := make(chan []PipelineEvent, 1)
	go func() {
		var events []PipelineEvent
		for event := range ch {
			events = append(events, event)
		}
		collected <- events
	}()
	err := engine.Run(ctx, opts, ch)
	close(ch)
	return <-collected, err
}

func assertTerminalContract(t *testing.T, events []PipelineEvent, runOutcome string, wantTargets map[string]string) {
	t.Helper()
	runTerminals := 0
	seenTargets := map[string]int{}
	for index, event := range events {
		if event.Done && event.RunOutcome == "" {
			t.Errorf("Done=true outside run terminal: %+v", event)
		}
		if event.RunOutcome != "" {
			runTerminals++
			if !event.Done || event.TargetOutcome != "" || event.RunOutcome != runOutcome {
				t.Errorf("invalid run terminal: %+v, want %s", event, runOutcome)
			}
			if index != len(events)-1 {
				t.Error("run terminal must be the final event")
			}
			assertTerminalWireError(t, event)
		}
		if event.TargetOutcome != "" {
			seenTargets[event.Target]++
			if event.Done || event.RunOutcome != "" {
				t.Errorf("target terminal prematurely finishes run: %+v", event)
			}
			if event.TargetOutcome != wantTargets[event.Target] {
				t.Errorf("target %q outcome = %q, want %q", event.Target, event.TargetOutcome, wantTargets[event.Target])
			}
			assertTerminalWireError(t, event)
		}
	}
	if runTerminals != 1 {
		t.Errorf("run terminal count = %d, want 1", runTerminals)
	}
	for target := range wantTargets {
		if seenTargets[target] != 1 {
			t.Errorf("target %q terminal count = %d, want 1", target, seenTargets[target])
		}
	}
}

func assertTerminalWireError(t *testing.T, event PipelineEvent) {
	t.Helper()
	wantsError := event.RunOutcome == "error" || event.RunOutcome == "cancelled" || event.TargetOutcome == "error" || event.TargetOutcome == "cancelled"
	if (event.Error != nil) != wantsError {
		t.Errorf("terminal %s/%s internal error = %v", event.RunOutcome, event.TargetOutcome, event.Error)
	}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if event.Error != nil {
		if wire["error"] != event.Error.Error() || event.ErrorMsg != event.Error.Error() {
			t.Errorf("terminal error missing from wire or ErrorMsg: %s / %q", data, event.ErrorMsg)
		}
	} else if _, present := wire["error"]; present {
		t.Errorf("successful/skipped terminal unexpectedly carries wire error: %s", data)
	}
}
