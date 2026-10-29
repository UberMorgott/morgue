package engine

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/UberMorgott/morgue/internal/recipe"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/runmodel"
	"github.com/UberMorgott/morgue/internal/util"
	"github.com/UberMorgott/morgue/internal/workspace"
)

func TestRunPersistsTerminalWorkspaceAndFreshRerun(t *testing.T) {
	input := terminalInput(t, "ok.dll")
	root := t.TempDir()
	e := New(config.Default(), t.TempDir())
	r, err := e.PrepareRun(context.Background(), runmodel.RunRequest{Path: input, Output: root, Recipe: terminalRecipeName})
	if err != nil {
		t.Fatal(err)
	}
	result, err := r.Execute(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	recovery, err := workspace.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	if recovery.Interrupted || recovery.Workspace.Run.Outcome == nil || *recovery.Workspace.Run.Outcome != result.Outcome || recovery.Workspace.LastSeq == 0 {
		t.Fatalf("bad terminal workspace: %+v", recovery)
	}
	before, err := os.ReadFile(filepath.Join(root, "morgue.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	r2, err := e.PrepareRun(context.Background(), runmodel.RunRequest{Path: input, Output: root, Recipe: terminalRecipeName})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(r2.Handle().Output) != root || r2.Handle().Output == root {
		t.Fatalf("rerun did not select fresh child: %+v", r2.Handle())
	}
	if _, err := r2.Execute(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(root, "morgue.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("rerun overwrote original workspace")
	}
}

func TestExternalArtifactRetainsExplicitJunctionPath(t *testing.T) {
	root := t.TempDir()
	physical := t.TempDir()
	alias := filepath.Join(t.TempDir(), "explicit-alias")
	if runtime.GOOS == "windows" {
		res, err := util.RunCmd(context.Background(), "cmd.exe", []string{"/d", "/c", "mklink", "/J", alias, physical}, "")
		if err != nil || res.ExitCode != 0 {
			t.Fatalf("create junction: %v / %+v", err, res)
		}
	} else if err := os.Symlink(physical, alias); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Remove(alias); err != nil {
			t.Error(err)
		}
	})
	file := filepath.Join(alias, "data.json")
	if err := os.WriteFile(file, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	path, external, err := productPath(root, alias, file)
	if err != nil || !external || path != filepath.ToSlash(file) {
		t.Fatalf("external alias lost: %q / %v / %v", path, external, err)
	}
}

type sharedGameRecipe struct{ terminalRecipe }

func (sharedGameRecipe) Name() string { return "__m2_shared_game" }
func (sharedGameRecipe) Execute(ctx *recipe.Context) error {
	path := filepath.Join(ctx.SharedOut, "_assets", "game", "raw-export")
	_, err := ctx.Shared.Do("game|assets=true", func() error {
		if err := os.MkdirAll(path, 0700); err != nil {
			return err
		}
		file, err := os.OpenFile(filepath.Join(path, "exports.txt"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = file.WriteString("one export\n")
		return errors.Join(err, file.Close())
	})
	if err != nil {
		return err
	}
	if err := ctx.Artifacts.Record(path, "asset", "extract-assets", "assetripper", true); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(ctx.Output, "fixture.txt"), []byte("target complete"), 0600)
}
func init() { recipe.Register(sharedGameRecipe{}) }

type mismatchedOriginalRecipe struct{ terminalRecipe }

func (mismatchedOriginalRecipe) Name() string { return "__m2_mismatched_original" }
func (mismatchedOriginalRecipe) Execute(ctx *recipe.Context) error {
	dir := filepath.Join(ctx.Output, "original")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, filepath.Base(ctx.Target)), []byte("different bytes"), 0600)
}
func init() { recipe.Register(mismatchedOriginalRecipe{}) }

type embeddedOutputRecipe struct{ terminalRecipe }

func (embeddedOutputRecipe) Name() string { return "__m2_embedded_output" }
func (embeddedOutputRecipe) Execute(ctx *recipe.Context) error {
	input := filepath.Join(ctx.Output, "extracted", "child.dll")
	out := filepath.Join(ctx.Output, "extracted", "child")
	if err := os.MkdirAll(filepath.Join(out, "src"), 0700); err != nil {
		return err
	}
	if err := os.WriteFile(input, []byte("child input"), 0600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, "src", "Game.cs"), []byte("class Game {}"), 0600); err != nil {
		return err
	}
	id, err := ctx.Children.Discover(input, out, terminalRecipeName)
	if err != nil {
		return err
	}
	return ctx.Children.Finish(id, "success", "")
}
func init() { recipe.Register(embeddedOutputRecipe{}) }

func TestRecipeManagedChildHasInputHashAndFileCounts(t *testing.T) {
	e := New(config.Default(), t.TempDir())
	r, err := e.PrepareRun(context.Background(), runmodel.RunRequest{Path: terminalInput(t, "ok.dll"), Output: t.TempDir(), Recipe: embeddedOutputRecipe{}.Name()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	recovery, err := workspace.Read(r.Handle().Output)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, target := range recovery.Workspace.Targets {
		if target.ParentTargetID != nil {
			found = true
			if target.SHA256 == nil || target.Files.Count == 0 {
				t.Fatalf("child metadata missing: %+v", target)
			}
		}
	}
	if !found {
		t.Fatal("child absent")
	}
}
func TestCapturedOriginalValidationFailsWholeRun(t *testing.T) {
	e := New(config.Default(), t.TempDir())
	r, err := e.PrepareRun(context.Background(), runmodel.RunRequest{Path: terminalInput(t, "ok.dll"), Output: t.TempDir(), Recipe: mismatchedOriginalRecipe{}.Name()})
	if err != nil {
		t.Fatal(err)
	}
	result, err := r.Execute(context.Background(), nil, nil)
	if err == nil || result.Outcome != "error" || result.Counts.Error != 1 || !strings.Contains(err.Error(), "captured original") {
		t.Fatalf("validation error did not fail whole run: %+v / %v", result, err)
	}
}

func TestSharedGameProductIsRecordedOncePerRun(t *testing.T) {
	e := New(config.Default(), t.TempDir())
	r, err := e.PrepareRun(context.Background(), runmodel.RunRequest{Path: terminalInput(t, "a.dll", "b.dll"), Output: t.TempDir(), Recipe: sharedGameRecipe{}.Name()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(r.Handle().Output, "_assets", "game", "raw-export", "exports.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "one export\n" {
		t.Fatalf("same live game re-exported assets: %s", data)
	}
	recovery, err := workspace.Read(r.Handle().Output)
	if err != nil {
		t.Fatal(err)
	}
	shared := 0
	for _, artifact := range recovery.Workspace.Artifacts {
		if artifact.TargetID == nil && artifact.Kind == "asset" {
			shared++
		}
	}
	if shared != 1 {
		t.Fatalf("shared artifact count=%d", shared)
	}
}

func TestCancelledRunLeavesSummaryAndJournal(t *testing.T) {
	e := New(config.Default(), t.TempDir())
	r, err := e.PrepareRun(context.Background(), runmodel.RunRequest{Path: terminalInput(t, "ok.dll"), Output: t.TempDir(), Recipe: terminalRecipeName})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := r.Execute(ctx, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	recovery, err := workspace.Read(r.Handle().Output)
	if err != nil {
		t.Fatal(err)
	}
	if recovery.Workspace.Run.Outcome == nil || *recovery.Workspace.Run.Outcome != "cancelled" || result.Outcome != "cancelled" {
		t.Fatalf("cancellation not recorded: %+v", recovery)
	}
	data, err := os.ReadFile(filepath.Join(r.Handle().Output, "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var summary PipelineSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyRunUsesDurableFreshWorkspace(t *testing.T) {
	e := New(config.Default(), t.TempDir())
	input := terminalInput(t, "ok.dll")
	root := t.TempDir()
	if err := e.Run(context.Background(), Options{Input: input, Output: root, Recipe: terminalRecipeName}, nil); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, "morgue.json"))
	if err != nil {
		t.Fatal(err)
	}
	ch := make(chan PipelineEvent, 1000)
	if err := e.Run(context.Background(), Options{Input: input, Output: root, Recipe: terminalRecipeName}, ch); err != nil {
		t.Fatal(err)
	}
	close(ch)
	actual := ""
	for ev := range ch {
		if ev.Done {
			actual = ev.OutputPath
		}
	}
	if filepath.Dir(actual) != root {
		t.Fatalf("legacy renderer did not receive fresh output: %s", actual)
	}
	after, err := os.ReadFile(filepath.Join(root, "morgue.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("legacy run overwrote workspace")
	}
}

type failingJournal struct {
	seen   []runmodel.Event
	err    error
	closed bool
}

type cancelOnMatchSink struct {
	workspace.Sink
	cancel context.CancelFunc
}

func (s *cancelOnMatchSink) Append(ev runmodel.Event) error {
	err := s.Sink.Append(ev)
	if err == nil && ev.Type == runmodel.EventLog {
		var p runmodel.LogRecord
		json.Unmarshal(ev.Data, &p)
		if p.Phase == "match" {
			s.cancel()
		}
	}
	return err
}

func (f *failingJournal) Append(ev runmodel.Event) error        { f.seen = append(f.seen, ev); return f.err }
func (f *failingJournal) Snapshot() (runmodel.Workspace, error) { return runmodel.Workspace{}, f.err }
func (f *failingJournal) Close() error                          { f.closed = true; return nil }

func TestJournalFailureNeverNotifiesSuccess(t *testing.T) {
	e := New(config.Default(), t.TempDir())
	r, err := e.PrepareRun(context.Background(), runmodel.RunRequest{Path: terminalInput(t, "ok.dll"), Output: t.TempDir(), Recipe: terminalRecipeName})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.store.Close(); err != nil {
		t.Fatal(err)
	}
	want := errors.New("disk full")
	fake := &failingJournal{err: want}
	r.store = fake
	ch := make(chan runmodel.Event, 100)
	result, err := r.Execute(context.Background(), nil, ch)
	close(ch)
	if !errors.Is(err, want) || result.Outcome != "error" || !fake.closed {
		t.Fatalf("journal failure swallowed: %+v / %v", result, err)
	}
	for ev := range ch {
		if ev.Type == runmodel.EventRunFinished {
			var p runmodel.RunFinished
			json.Unmarshal(ev.Data, &p)
			if p.Outcome == "success" {
				t.Fatal("uncommitted success was notified")
			}
		}
	}
}
