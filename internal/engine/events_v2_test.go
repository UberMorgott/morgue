package engine

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/recipe"
	"github.com/UberMorgott/morgue/internal/recon"
	"github.com/UberMorgott/morgue/internal/runmodel"
)

func TestTypedProgressAndLegacyErrorProjection(t *testing.T) {
	e := New(config.Default(), t.TempDir())
	r, err := e.PrepareRun(context.Background(), runmodel.RunRequest{Path: terminalInput(t, "ok.dll"), Output: t.TempDir(), Recipe: terminalRecipeName})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	// These are in-memory sequencer tests; integrated durability is covered by
	// workspace_test. A real Writer correctly rejects progress before started.
	if err := r.store.Close(); err != nil {
		t.Fatal(err)
	}
	r.store = nil
	opts := r.options(nil)
	ch := make(chan runmodel.Event, 10)
	r.events = ch
	path := opts.Input + "/ok.dll"
	r.discover(opts, TargetResult{Recon: recon.Result{Path: path, Kind: recon.Managed}, Recipe: terminalRecipe{}})
	em := emitter{run: r, opts: opts}
	want := errors.New("decompiler warning")
	em.send(PipelineEvent{Phase: "execute", Target: path, Progress: &recipe.StepProgress{Step: 2, Total: 4, Name: "Decompile", Tool: "ilspycmd", Count: 3, CountTotal: 7, Unit: "types", Status: recipe.Warn, Duration: 1500 * time.Millisecond, Error: want}})
	em.send(PipelineEvent{Phase: "download", Tool: "ilspycmd", ToolStage: "download", Bytes: 123, BytesTotal: 456, Message: "deliberately misleading 99% text"})
	close(ch)
	<-ch
	step := <-ch
	tool := <-ch
	var p runmodel.StepProgress
	if err := json.Unmarshal(step.Data, &p); err != nil {
		t.Fatal(err)
	}
	if p.Count != 3 || p.CountTotal != 7 || p.DurationMs != 1500 || p.Status != "warn" || p.Error != want.Error() || p.StepID != "step-2" {
		t.Fatalf("lost typed progress: %+v", p)
	}
	legacy := LegacyEvent(step)
	if legacy.Severity != "warn" || legacy.Progress == nil || legacy.Progress.Error.Error() != want.Error() {
		t.Fatalf("lost legacy warning: %+v", legacy)
	}
	var download runmodel.ToolProgress
	if err := json.Unmarshal(tool.Data, &download); err != nil {
		t.Fatal(err)
	}
	if download.Bytes != 123 || download.BytesTotal != 456 {
		t.Fatalf("parsed log instead of fields: %+v", download)
	}
	terminal, err := runmodel.NewEvent(r.Handle().ID, 4, time.Now().UTC(), "t1", "", runmodel.TargetFinished{Outcome: "error", Error: "wire-safe failure"})
	if err != nil {
		t.Fatal(err)
	}
	if projected := LegacyEvent(terminal); projected.ErrorMsg != "wire-safe failure" || projected.Error.Error() != "wire-safe failure" {
		t.Fatalf("lost legacy error: %+v", projected)
	}
}

func TestConcurrentEventSequencer(t *testing.T) {
	e := New(config.Default(), t.TempDir())
	r, err := e.PrepareRun(context.Background(), runmodel.RunRequest{Path: terminalInput(t, "ok.dll"), Output: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	if err := r.store.Close(); err != nil {
		t.Fatal(err)
	}
	r.store = nil
	ch := make(chan runmodel.Event, 100)
	r.events = ch
	em := emitter{run: r, opts: r.options(nil)}
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() { em.emit("log", "", "concurrent event") })
	}
	wg.Wait()
	close(ch)
	seq := uint64(0)
	for ev := range ch {
		seq++
		if ev.Seq != seq {
			t.Fatalf("out of order: %d != %d", ev.Seq, seq)
		}
	}
	if seq != 100 {
		t.Fatalf("lost events with sufficient buffer: %d", seq)
	}
}
