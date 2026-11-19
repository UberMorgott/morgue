package engine

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/recipe"
	"github.com/UberMorgott/morgue/internal/recon"
	"github.com/UberMorgott/morgue/internal/runmodel"
)

func TestPrepareRunFreezesDefaultsAndAdmission(t *testing.T) {
	cfg := config.Default()
	cfg.AllowDynamicExecution = true
	e := New(cfg, t.TempDir())
	input := terminalInput(t, "ok.dll")
	output := t.TempDir()
	off := false
	req := runmodel.RunRequest{Path: input, Output: output, Recipe: terminalRecipeName, AllowDynamic: &off, ReferencePaths: []string{input}}
	r, err := e.PrepareRun(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	s := r.Spec()
	if s.AllowDynamic || s.Cflow || !s.ExtractAssets || len(s.ReferencePaths) != 1 {
		t.Fatalf("bad defaults: %+v", s)
	}
	s.ReferencePaths[0] = "mutated"
	req.ReferencePaths[0] = "changed"
	if r.Spec().ReferencePaths[0] != input {
		t.Fatal("run spec aliases caller-owned slices")
	}
	req.ReferencePaths[0] = input
	if _, err := e.PrepareRun(context.Background(), req); !errors.Is(err, ErrOutputBusy) {
		t.Fatalf("second admission=%v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(context.Background(), nil, nil); err == nil {
		t.Fatal("closed admission executed")
	}
	r2, err := e.PrepareRun(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	_ = r2.Close()
}

func TestPrepareRunRejectsInvalidRequest(t *testing.T) {
	e := New(config.Default(), t.TempDir())
	input := terminalInput(t, "ok.dll")
	for _, req := range []runmodel.RunRequest{{}, {Path: "missing"}, {Path: input, Recipe: "missing"}, {Path: input, Exclude: []string{"["}}, {Path: input, ReferencePaths: []string{"missing"}}} {
		if _, err := e.PrepareRun(context.Background(), req); !errors.Is(err, ErrInvalidRun) {
			t.Fatalf("request %+v: %v", req, err)
		}
	}
}

func TestPrepareRunRejectsAmbiguousGameDataDestination(t *testing.T) {
	input := t.TempDir()
	for _, game := range []string{"one", "two"} {
		managed := filepath.Join(input, game, game+"_Data", "Managed")
		if err := os.MkdirAll(managed, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(managed, "Game.dll"), []byte("inert"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	e := New(config.Default(), t.TempDir())
	r, err := e.PrepareRun(context.Background(), runmodel.RunRequest{Path: input, Output: t.TempDir(), GameDataOut: t.TempDir()})
	if err == nil {
		_ = r.Close()
		t.Fatal("two games admitted with one explicit game-data destination")
	}
	if !errors.Is(err, ErrInvalidRun) {
		t.Fatalf("wrong rejection: %v", err)
	}
}

func TestPreparedRunLifecycleAndDuplicateBasenames(t *testing.T) {
	input := t.TempDir()
	for _, sub := range []string{"a", "b"} {
		dir := filepath.Join(input, sub)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "ok.dll"), []byte("inert"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	e := New(config.Default(), t.TempDir())
	r, err := e.PrepareRun(context.Background(), runmodel.RunRequest{Path: input, Output: t.TempDir(), Recipe: terminalRecipeName})
	if err != nil {
		t.Fatal(err)
	}
	ch := make(chan runmodel.Event, 1000)
	result, err := r.Execute(context.Background(), nil, ch)
	if err != nil {
		t.Fatal(err)
	}
	close(ch)
	if result.Outcome != "success" || result.Counts.Success != 2 || len(result.Targets) != 2 || result.Targets[0].ID == result.Targets[1].ID || result.Targets[0].Output == result.Targets[1].Output {
		t.Fatalf("bad result: %+v", result)
	}
	var events []runmodel.Event
	for ev := range ch {
		events = append(events, ev)
	}
	assertV2Lifecycle(t, r.Handle(), events, "success")
	if _, err := r.Execute(context.Background(), nil, nil); err == nil {
		t.Fatal("run executed twice")
	}
}

func TestPreparedRunCancellationAndAbsentConsumer(t *testing.T) {
	e := New(config.Default(), t.TempDir())
	r, err := e.PrepareRun(context.Background(), runmodel.RunRequest{Path: terminalInput(t, "ok.dll"), Output: t.TempDir(), Recipe: terminalRecipeName})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ch := make(chan runmodel.Event, 100)
	result, err := r.Execute(ctx, nil, ch)
	close(ch)
	if !errors.Is(err, context.Canceled) || result.Outcome != "cancelled" {
		t.Fatalf("cancelled result: %+v / %v", result, err)
	}
	var events []runmodel.Event
	for ev := range ch {
		events = append(events, ev)
	}
	assertV2Lifecycle(t, r.Handle(), events, "cancelled")
	for _, sink := range []chan runmodel.Event{nil, make(chan runmodel.Event)} {
		r, err := e.PrepareRun(context.Background(), runmodel.RunRequest{Path: terminalInput(t, "ok.dll"), Output: t.TempDir(), Recipe: terminalRecipeName})
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { _, err := r.Execute(context.Background(), nil, sink); done <- err }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("absent/slow consumer blocked execution")
		}
	}
}

type cancelRunKey struct{}

func TestPreparedRunCancellationDuringAnalysisPreservesScannedTargets(t *testing.T) {
	e := New(config.Default(), t.TempDir())
	r, err := e.PrepareRun(context.Background(), runmodel.RunRequest{Path: terminalInput(t, "a.dll", "b.dll"), Output: t.TempDir(), Recipe: terminalRecipeName})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.store = &cancelOnMatchSink{Sink: r.store, cancel: cancel}
	ch := make(chan runmodel.Event, 1000)
	result, err := r.Execute(ctx, nil, ch)
	close(ch)
	if !errors.Is(err, context.Canceled) || result.Counts.Cancelled != 2 || len(result.Targets) != 2 {
		t.Fatalf("scanned targets lost during analysis: %+v / %v", result, err)
	}
	var events []runmodel.Event
	for event := range ch {
		events = append(events, event)
	}
	assertV2Lifecycle(t, r.Handle(), events, "cancelled")
}

type cancelRunRecipe struct{ terminalRecipe }

func (cancelRunRecipe) Name() string { return "__cancel_analyzed_targets" }
func (cancelRunRecipe) Execute(ctx *recipe.Context) error {
	if cancel, ok := ctx.Ctx.Value(cancelRunKey{}).(context.CancelFunc); ok {
		cancel()
	}
	return ctx.Ctx.Err()
}
func init() { recipe.Register(cancelRunRecipe{}) }

func TestPreparedRunCancellationFinishesPendingTargets(t *testing.T) {
	e := New(config.Default(), t.TempDir())
	r, err := e.PrepareRun(context.Background(), runmodel.RunRequest{Path: terminalInput(t, "a.dll", "b.dll"), Output: t.TempDir(), Recipe: cancelRunRecipe{}.Name()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = context.WithValue(ctx, cancelRunKey{}, cancel)
	ch := make(chan runmodel.Event, 1000)
	result, err := r.Execute(ctx, nil, ch)
	close(ch)
	if !errors.Is(err, context.Canceled) || result.Counts.Cancelled != 2 || result.Counts.Running != 0 {
		t.Fatalf("pending target lost on cancel: %+v / %v", result, err)
	}
	finished := map[string]int{}
	var events []runmodel.Event
	for event := range ch {
		events = append(events, event)
		if event.Type == runmodel.EventTargetFinished {
			finished[event.TargetID]++
		}
	}
	for _, target := range result.Targets {
		if finished[target.ID] != 1 {
			t.Fatalf("target terminal count: %+v / %v", target, finished)
		}
	}
	assertV2Lifecycle(t, r.Handle(), events, "cancelled")
}

func assertV2Lifecycle(t *testing.T, h runmodel.RunHandle, events []runmodel.Event, want string) {
	t.Helper()
	if len(events) < 2 || events[0].Type != runmodel.EventRunStarted || events[len(events)-1].Type != runmodel.EventRunFinished {
		t.Fatalf("bad lifecycle: %+v", events)
	}
	terminals := 0
	for i, ev := range events {
		if ev.Seq != uint64(i+1) || ev.Run != h.ID {
			t.Fatalf("unordered event %+v", ev)
		}
		if ev.Type == runmodel.EventRunFinished {
			terminals++
			var p runmodel.RunFinished
			if err := json.Unmarshal(ev.Data, &p); err != nil {
				t.Fatal(err)
			}
			if p.Outcome != want {
				t.Fatalf("outcome %s", p.Outcome)
			}
		}
	}
	if terminals != 1 {
		t.Fatalf("terminal count %d", terminals)
	}
}

const nestedFixtureName = "__run_nested_fixture"

type nestedFixtureRecipe struct{}

func (nestedFixtureRecipe) Name() string        { return nestedFixtureName }
func (nestedFixtureRecipe) Description() string { return "inert nested target fixture" }
func (nestedFixtureRecipe) Match(r *recon.Result) bool {
	return r.Kind == recon.NSIS && filepath.Base(r.Path) == "__m1_child.exe"
}
func (nestedFixtureRecipe) Steps() []recipe.StepInfo { return nil }
func (nestedFixtureRecipe) RequiredTools() []string  { return nil }
func (nestedFixtureRecipe) Execute(ctx *recipe.Context) error {
	if strings.HasPrefix(filepath.Base(ctx.Target), "fail") {
		return errTerminalRecipe
	}
	if strings.HasPrefix(filepath.Base(ctx.Target), "__m1_") {
		dir := filepath.Join(ctx.Output, "extracted")
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		name := "__m1_child.exe"
		data, err := os.ReadFile(ctx.Target)
		if err != nil {
			return err
		}
		if filepath.Base(ctx.Target) == "__m1_child.exe" {
			name = "mscorlib.dll"
			data = []byte("inert skipped grandchild")
		}
		return os.WriteFile(filepath.Join(dir, name), data, 0600)
	}
	return nil
}

func init() { recipe.RegisterFirst(nestedFixtureRecipe{}) }

func TestPreparedRunNestedNSISGrandchildren(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	// Append a recognizable but inert NSIS header to an existing valid PE.
	header := make([]byte, 228)
	copy(header[4:], []byte{0xef, 0xbe, 0xad, 0xde, 'N', 'u', 'l', 'l', 's', 'o', 'f', 't', 'I', 'n', 's', 't'})
	binary.LittleEndian.PutUint32(header[20:], 100)
	binary.LittleEndian.PutUint32(header[24:], 228)
	input := filepath.Join(t.TempDir(), "__m1_root.exe")
	if err := os.WriteFile(input, append(data, header...), 0600); err != nil {
		t.Fatal(err)
	}
	e := New(config.Default(), t.TempDir())
	r, err := e.PrepareRun(context.Background(), runmodel.RunRequest{Path: input, Output: t.TempDir(), Recipe: nestedFixtureName})
	if err != nil {
		t.Fatal(err)
	}
	ch := make(chan runmodel.Event, 1000)
	result, err := r.Execute(context.Background(), nil, ch)
	close(ch)
	if err == nil || result.Outcome != "error" || len(result.Targets) != 3 || result.Counts.Error != 2 || result.Counts.Skipped != 1 || result.LeafTargets.Total != 1 {
		t.Fatalf("nested outcomes: %+v / %v", result, err)
	}
	byID := map[string]runmodel.TargetResult{}
	for _, target := range result.Targets {
		byID[target.ID] = target
	}
	for _, target := range result.Targets {
		if target.ParentTargetID != "" {
			if _, ok := byID[target.ParentTargetID]; !ok {
				t.Fatalf("orphan target: %+v", target)
			}
		}
	}
	var events []runmodel.Event
	for event := range ch {
		events = append(events, event)
	}
	assertV2Lifecycle(t, r.Handle(), events, "error")
	summaryData, err := os.ReadFile(filepath.Join(r.Handle().Output, "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var summary PipelineSummary
	if err := json.Unmarshal(summaryData, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Stats.Total != 3 {
		t.Fatalf("root summary dropped children: %s", summaryData)
	}
}

// The nested fixture uses the child observer as well as the real unpack runner;
// no game code, external tools or network are involved.
func TestPreparedRunChildObserverFailurePropagates(t *testing.T) {
	e := New(config.Default(), t.TempDir())
	r, err := e.PrepareRun(context.Background(), runmodel.RunRequest{Path: terminalInput(t, "ok.dll"), Output: t.TempDir(), Recipe: terminalRecipeName})
	if err != nil {
		t.Fatal(err)
	}
	opts := r.options(nil)
	parent := TargetResult{Recon: recon.Result{Path: filepath.Join(opts.Input, "ok.dll"), Kind: recon.Managed}, Recipe: terminalRecipe{}, Output: filepath.Join(opts.Output, "ok")}
	r.discover(opts, parent)
	observer := r.children(opts, parent.Recon.Path)
	id, err := observer.Discover(filepath.Join(parent.Output, "extracted", "fail.dll"), filepath.Join(parent.Output, "extracted", "fail"), terminalRecipeName)
	if err != nil {
		t.Fatal(err)
	}
	if err := observer.Finish(id, "error", "child failed"); err != nil {
		t.Fatal(err)
	}
	if err := observer.Finish(id, "success", ""); err == nil {
		t.Fatal("child finished twice")
	}
	if err := r.descendantError(r.targetID(opts, parent.Recon.Path)); err == nil {
		t.Fatal("child failure lost")
	}
	_ = r.Close()
}
