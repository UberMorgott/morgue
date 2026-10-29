package services

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/UberMorgott/morgue/internal/runmodel"
	"github.com/UberMorgott/morgue/internal/workspace"
)

const testRun = "0123456789abcdef0123456789abcdef"

func startedEvent(t *testing.T, root string) runmodel.Event {
	t.Helper()
	ts := time.Now().UTC()
	ev, err := runmodel.NewEvent(testRun, 1, ts, "", "", runmodel.RunStarted{
		Handle: runmodel.RunHandle{ID: testRun, StartedAt: ts, Output: root},
		Spec:   runmodel.RunSpec{Version: 1, Path: "in", Output: root, Exclude: []string{}, ReferencePaths: []string{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

// fakeJournal serves n events with sequence 1..n in pages.
func fakeJournal(n uint64) WorkspaceReader {
	return WorkspaceReader{
		Read: func(root string) (workspace.Recovery, error) {
			return workspace.Recovery{LastSeq: n}, nil
		},
		ReadEvents: func(root, runID string, after uint64, limit int) (workspace.EventPage, error) {
			p := workspace.EventPage{RunID: runID, LastSeq: n}
			for s := after + 1; s <= n && len(p.Events) < limit; s++ {
				p.Events = append(p.Events, runmodel.Event{Version: 2, Run: runID, Seq: s, Type: runmodel.EventLog, Data: json.RawMessage(`{}`)})
			}
			p.HasMore = len(p.Events) > 0 && p.Events[len(p.Events)-1].Seq < n
			return p, nil
		},
	}
}

func TestWorkspaceServiceEventsBoundedAndRunChecked(t *testing.T) {
	s := NewWorkspaceService(fakeJournal(2500), workspace.NewRecentStore(filepath.Join(t.TempDir(), "r.json")))
	if _, err := s.Events(testRun, 0); !errors.Is(err, ErrNoCurrentRun) {
		t.Fatalf("before start: %v", err)
	}
	ObserveWorkspaceEvent(s, startedEvent(t, t.TempDir()))
	if _, err := s.Events("ffffffffffffffffffffffffffffffff", 0); !errors.Is(err, ErrStaleRun) {
		t.Fatalf("stale run: %v", err)
	}
	p, err := s.EventsLimit(testRun, 0, 5000)
	if err != nil || len(p.Events) != ReplayPageSize || !p.HasMore {
		t.Fatalf("page len=%d more=%v err=%v", len(p.Events), p.HasMore, err)
	}
	var got []uint64
	last, err := ReplayEvents(s, testRun, 10, func(ev runmodel.Event) error { got = append(got, ev.Seq); return nil })
	if err != nil || last != 2500 || len(got) != 2490 || got[0] != 11 {
		t.Fatalf("replay last=%d n=%d err=%v", last, len(got), err)
	}
	for i := 1; i < len(got); i++ {
		if got[i] != got[i-1]+1 {
			t.Fatalf("gap/dup at %d", i)
		}
	}
}

func TestWorkspaceServiceRecentResolvesDiskStatus(t *testing.T) {
	store := workspace.NewRecentStore(filepath.Join(t.TempDir(), "r.json"))
	base := t.TempDir()
	mk := func(name string, files ...string) string {
		dir := filepath.Join(base, name)
		for _, f := range files {
			p := filepath.Join(dir, filepath.FromSlash(f))
			_ = os.MkdirAll(filepath.Dir(p), 0o755)
			_ = os.WriteFile(p, []byte("{}"), 0o644)
		}
		_ = os.MkdirAll(dir, 0o755)
		return dir
	}
	done := mk("done", "morgue.json", ".morgue/journal.ndjson")
	crashed := mk("crashed", ".morgue/journal.ndjson")
	legacy := mk("legacy", "summary.json")
	missing := filepath.Join(base, "moved")
	for _, p := range []string{missing, legacy, crashed, done} {
		if err := store.Add(p); err != nil {
			t.Fatal(err)
		}
	}
	ok := "success"
	reader := WorkspaceReader{Read: func(root string) (workspace.Recovery, error) {
		r := workspace.Recovery{}
		r.Workspace.Spec.Path = "game.exe"
		switch root {
		case done:
			r.Workspace.Run.Outcome = &ok
		case crashed:
			r.Interrupted = true
		}
		return r, nil
	}}
	got := NewWorkspaceService(reader, store).Recent()
	want := map[string]string{done: "finished", crashed: "interrupted", legacy: "legacy", missing: "unavailable"}
	if len(got) != 4 {
		t.Fatalf("got %d", len(got))
	}
	for _, r := range got {
		if want[r.Path] != r.Status {
			t.Errorf("%s: status %q want %q", r.Path, r.Status, want[r.Path])
		}
	}
	if got[0].Path != done || got[0].Outcome != "success" || got[0].Input != "game.exe" {
		t.Errorf("first = %+v", got[0])
	}
}
