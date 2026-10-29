package workspace

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/UberMorgott/morgue/internal/runmodel"
)

var t0 = time.Date(2026, 10, 29, 1, 2, 3, 0, time.UTC)

// run builds a contiguous, contract-valid event stream for one handle.
type run struct {
	t    testing.TB
	h    runmodel.RunHandle
	spec runmodel.RunSpec
	seq  uint64
	ts   time.Time
}

func newRun(t testing.TB, root, id string) *run {
	t.Helper()
	h := runmodel.RunHandle{ID: id, StartedAt: t0, Output: root}
	spec := runmodel.RunSpec{Version: runmodel.SpecVersion, Path: filepath.Join(filepath.Dir(root), "game"), Output: root, Exclude: []string{}, ReferencePaths: []string{}}
	return &run{t: t, h: h, spec: spec, ts: t0}
}

func (r *run) ev(target, parent string, p runmodel.Payload) runmodel.Event {
	r.t.Helper()
	r.seq++
	if r.seq > 1 {
		r.ts = r.ts.Add(time.Millisecond)
	}
	ev, err := runmodel.NewEvent(r.h.ID, r.seq, r.ts, target, parent, p)
	if err != nil {
		r.t.Fatalf("event %d: %v", r.seq, err)
	}
	return ev
}

func (r *run) started() runmodel.Event {
	return r.ev("", "", runmodel.RunStarted{Handle: r.h, Spec: r.spec})
}
func (r *run) discovered(id, parent, out string) runmodel.Event {
	return r.ev(id, parent, runmodel.TargetDiscovered{InputPath: "C:/game/" + id, RelativeInput: id, Kind: "pe", Recipe: "native", Output: out})
}
func (r *run) progress(id string) runmodel.Event {
	return r.ev(id, "", runmodel.StepProgress{StepID: "s1", Name: "decompile", Status: "running", StepIndex: 1, StepTotal: 2})
}
func (r *run) logLine(msg string) runmodel.Event {
	return r.ev("", "", runmodel.LogRecord{Level: "info", Message: msg})
}
func (r *run) targetDone(id, parent, outcome string) runmodel.Event {
	return r.ev(id, parent, runmodel.TargetFinished{Outcome: outcome, Recipe: "native", DurationMs: 5, Files: &runmodel.FileCounts{Count: 2, Bytes: 10}})
}
func (r *run) finished(outcome string, c runmodel.Counts) runmodel.Event {
	return r.ev("", "", runmodel.RunFinished{Outcome: outcome, FinishedAt: r.ts.Add(time.Millisecond), DurationMs: r.ts.Sub(t0).Milliseconds() + 1, Targets: c, LeafTargets: c})
}

const runA = "0123456789abcdef0123456789abcdef"
const runB = "fedcba9876543210fedcba9876543210"

func mustAppend(t testing.TB, w *Writer, evs ...runmodel.Event) {
	t.Helper()
	for _, ev := range evs {
		if err := w.Append(ev); err != nil {
			t.Fatalf("append %d (%s): %v", ev.Seq, ev.Type, err)
		}
	}
}

func manifestOnDisk(t testing.TB, root string) (runmodel.Workspace, []byte) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, runmodel.ManifestPath))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var ws runmodel.Workspace
	if err := json.Unmarshal(b, &ws); err != nil {
		t.Fatalf("manifest is not valid JSON: %v", err)
	}
	return ws, b
}

func openRun(t testing.TB, id string) (*Writer, *run, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "out")
	r := newRun(t, root, id)
	w, err := Open(r.h, r.spec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w, r, root
}

func TestWriterNormalRunManifestAgreesWithJournal(t *testing.T) {
	w, r, root := openRun(t, runA)
	var published []uint64
	w.SetPublisher(func(ws runmodel.Workspace) error { published = append(published, ws.LastSeq); return nil })

	if _, err := w.Snapshot(); !errors.Is(err, ErrNoEvents) {
		t.Fatalf("snapshot before events: %v, want ErrNoEvents", err)
	}
	if _, err := os.Stat(filepath.Join(root, runmodel.ManifestPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manifest published before first event: %v", err)
	}
	lock, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(runmodel.RunToolsLockPath)))
	if err != nil || strings.TrimSpace(string(lock)) != `{"schemaVersion":1,"complete":false,"tools":[]}` {
		t.Fatalf("tools lock = %q, %v", lock, err)
	}

	gameOut := filepath.Join(root, "game")
	mustAppend(t, w, r.started(), r.discovered("t1", "", gameOut), r.discovered("t2", "t1", filepath.Join(gameOut, "child")))
	if ws, _ := manifestOnDisk(t, root); ws.LastSeq != 3 || len(ws.Targets) != 2 || ws.Run.Targets.Pending != 2 {
		t.Fatalf("discovery not published synchronously: lastSeq=%d targets=%d", ws.LastSeq, len(ws.Targets))
	}
	mustAppend(t, w, r.progress("t2"))
	if ws, _ := manifestOnDisk(t, root); ws.LastSeq != 3 {
		t.Fatalf("progress published synchronously (lastSeq %d); it must be coalesced", ws.LastSeq)
	}
	tid := "t2"
	sum := strings.Repeat("a", 64)
	mustAppend(t, w,
		r.ev("t2", "t1", runmodel.ArtifactWritten{Artifact: runmodel.Artifact{ID: "a1", TargetID: &tid, Path: filepath.Join(gameOut, "child", "src"), Kind: "decompiled-source", SHA256: &sum, HashStatus: "complete", Complete: true}}),
		r.ev("t2", "t1", runmodel.ArtifactWritten{Artifact: runmodel.Artifact{ID: "a1", TargetID: &tid, Path: "game/child/src", Kind: "decompiled-source", HashStatus: "pending"}}),
		r.targetDone("t2", "t1", "success"),
		r.targetDone("t1", "", "error"),
	)
	counts := runmodel.Counts{Total: 2, Success: 1, Error: 1}
	mustAppend(t, w, r.finished("error", counts))
	if err := w.Append(r.logLine("late")); err == nil {
		t.Fatal("append after run.finished accepted")
	}
	r.seq-- // the rejected record was not written
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	rec, err := Read(root)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Interrupted || rec.Active || rec.TornTail || rec.LastSeq != 9 {
		t.Fatalf("recovery = %+v", rec)
	}
	ws, raw := manifestOnDisk(t, root)
	want, _ := encodeManifest(rec.Workspace)
	if string(raw) != string(want) {
		t.Fatalf("manifest differs from journal replay\nmanifest %s\nreplay   %s", raw, want)
	}
	if ws.Run.State != "finished" || ws.Run.Outcome == nil || *ws.Run.Outcome != "error" || ws.Run.FinishedAt == nil || ws.Run.Targets != counts {
		t.Fatalf("run record = %+v", ws.Run)
	}
	t1, t2 := ws.Targets[0], ws.Targets[1]
	if t1.Output != "game" || t2.Output != "game/child" || t2.ParentTargetID == nil || *t2.ParentTargetID != "t1" || t1.ParentTargetID != nil {
		t.Fatalf("target paths/parents: %+v / %+v", t1, t2)
	}
	if t2.StartedAt == nil || t2.FinishedAt == nil || t2.Files.Count != 2 || t2.Quality != nil || t2.InputSet == nil {
		t.Fatalf("target t2 = %+v", t2)
	}
	if len(ws.Artifacts) != 1 || ws.Artifacts[0].Path != "game/child/src" || ws.Artifacts[0].HashStatus != "pending" || ws.Artifacts[0].External {
		t.Fatalf("artifacts not upserted by id: %+v", ws.Artifacts)
	}
	if len(ws.Warnings) != 0 || ws.Journal != runmodel.JournalPath || ws.ToolsLock != runmodel.RunToolsLockPath {
		t.Fatalf("root fields: %+v", ws)
	}
	if fmt.Sprint(published) != "[1 2 3 4 6 7 8 9]" && fmt.Sprint(published) != "[1 2 3 5 6 7 8 9]" {
		t.Fatalf("publisher seqs = %v", published)
	}
	journal, _ := os.ReadFile(journalPath(root))
	if len(journal) == 0 || journal[0] == 0xEF || strings.Count(string(journal), "\n") != 9 || !strings.HasSuffix(string(journal), "}\n") {
		t.Fatalf("journal framing: %q", journal)
	}
}

func TestWriterRejectsContractViolationsWithoutWriting(t *testing.T) {
	w, r, root := openRun(t, runA)
	other := newRun(t, root, runB)
	bad := r.discovered("t1", "", "")
	r.seq = 0
	if err := w.Append(bad); err == nil {
		t.Fatal("first record other than run.started accepted")
	}
	r.seq, r.ts = 0, t0
	wrongSpec := *r
	wrongSpec.spec.Cflow = true
	if err := w.Append(wrongSpec.started()); err == nil {
		t.Fatal("run.started with a different spec accepted")
	}
	if err := w.Append(other.started()); err == nil {
		t.Fatal("foreign run accepted")
	}
	mustAppend(t, w, r.started())
	gap := r.discovered("t1", "", "")
	gap.Seq = 5
	if err := w.Append(gap); err == nil {
		t.Fatal("sequence gap accepted")
	}
	dup := r.discovered("t1", "", "")
	dup.Seq = 1
	if err := w.Append(dup); err == nil {
		t.Fatal("duplicate sequence accepted")
	}
	r.seq = 1
	v3 := r.discovered("t1", "", "")
	v3.Version = 3
	if err := w.Append(v3); err == nil {
		t.Fatal("unsupported major version accepted")
	}
	r.seq = 1
	escape := r.discovered("t1", "", filepath.Join(root, "..", "elsewhere"))
	if err := w.Append(escape); err == nil || !errors.Is(err, errPathEscape) {
		t.Fatalf("escaping output: %v", err)
	}
	r.seq = 1
	mustAppend(t, w, r.discovered("t1", "", "game")) // still usable
	rec, err := Read(root)
	if err != nil || rec.LastSeq != 2 || !rec.Active {
		t.Fatalf("read = %+v, %v", rec, err)
	}
}

func TestOpenRequiresEmptyOutputAndLeavesNonEmptyUntouched(t *testing.T) {
	root := t.TempDir()
	keep := filepath.Join(root, "old.txt")
	if err := os.WriteFile(keep, []byte("v0.7 output"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := newRun(t, root, runA)
	if _, err := Open(r.h, r.spec); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("open nonempty: %v, want ErrNotEmpty", err)
	}
	if b, _ := os.ReadFile(keep); string(b) != "v0.7 output" {
		t.Fatal("existing output modified")
	}
	if _, err := os.Stat(filepath.Join(root, morgueDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("scaffolding left in nonempty output: %v", err)
	}

	scaff := t.TempDir()
	if err := os.MkdirAll(filepath.Join(scaff, morgueDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scaff, morgueDir, lockFileName), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r2 := newRun(t, scaff, runB)
	w, err := Open(r2.h, r2.spec)
	if err != nil {
		t.Fatalf("lock scaffolding counted as content: %v", err)
	}
	_ = w.Close()
}

func TestOpenLockedWinsOverEmptiness(t *testing.T) {
	w, r, root := openRun(t, runA)
	mustAppend(t, w, r.started())
	r2 := newRun(t, root, runB)
	if _, err := Open(r2.h, r2.spec); !errors.Is(err, ErrLocked) {
		t.Fatalf("second open: %v, want ErrLocked", err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(r2.h, r2.spec); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("open after close: %v, want ErrNotEmpty", err)
	}
}

type faultyJournal struct {
	f                   journalFile
	writes, syncs       int
	failWrite, failSync int
}

func (j *faultyJournal) WriteAt(p []byte, off int64) (int, error) {
	j.writes++
	if j.writes == j.failWrite {
		n, _ := j.f.WriteAt(p[:len(p)/2], off) // torn write, then disk full
		return n, errors.New("simulated disk full")
	}
	return j.f.WriteAt(p, off)
}
func (j *faultyJournal) Sync() error {
	j.syncs++
	if j.syncs == j.failSync {
		return errors.New("simulated sync failure")
	}
	return j.f.Sync()
}
func (j *faultyJournal) Truncate(n int64) error { return j.f.Truncate(n) }
func (j *faultyJournal) Close() error           { return j.f.Close() }

func withFaultyJournal(t *testing.T, fj *faultyJournal) {
	old := createJournal
	createJournal = func(p string) (journalFile, error) {
		f, err := old(p)
		fj.f = f
		return fj, err
	}
	t.Cleanup(func() { createJournal = old })
}

// A durability failure keeps the previous valid manifest and the journal
// prefix, never advances the snapshot and forbids a later successful finish.
func assertFailedDurably(t *testing.T, w *Writer, r *run, root string) {
	t.Helper()
	if _, err := w.Snapshot(); err == nil {
		t.Fatal("snapshot after durability failure succeeded")
	}
	r.seq = 1
	if err := w.Append(r.finished("success", runmodel.Counts{})); err == nil {
		t.Fatal("successful completion accepted after durability failure")
	}
	if err := w.Close(); err == nil {
		t.Fatal("close hid the durability failure")
	}
	ws, _ := manifestOnDisk(t, root)
	if ws.LastSeq != 1 || len(ws.Targets) != 0 {
		t.Fatalf("prior manifest not kept: lastSeq=%d targets=%d", ws.LastSeq, len(ws.Targets))
	}
	rec, err := Read(root)
	if err != nil || rec.LastSeq != 1 || rec.TornTail || !rec.Interrupted {
		t.Fatalf("journal prefix after failure: %+v, %v", rec, err)
	}
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

func TestAppendWriteFailureRollsBack(t *testing.T) {
	fj := &faultyJournal{failWrite: 2}
	withFaultyJournal(t, fj)
	w, r, root := openRun(t, runA)
	mustAppend(t, w, r.started())
	if err := w.Append(r.discovered("t1", "", "game")); err == nil {
		t.Fatal("write failure not reported")
	}
	assertFailedDurably(t, w, r, root)
}

func TestAppendSyncFailureRollsBack(t *testing.T) {
	fj := &faultyJournal{failSync: 2}
	withFaultyJournal(t, fj)
	w, r, root := openRun(t, runA)
	mustAppend(t, w, r.started())
	if err := w.Append(r.discovered("t1", "", "game")); err == nil {
		t.Fatal("sync failure not reported")
	}
	assertFailedDurably(t, w, r, root)
}

func TestManifestReplaceFailureKeepsPriorSnapshot(t *testing.T) {
	w, r, root := openRun(t, runA)
	mustAppend(t, w, r.started())
	old := replaceFile
	replaceFile = func(string, string) error { return errors.New("simulated replace failure") }
	t.Cleanup(func() { replaceFile = old })
	if err := w.Append(r.discovered("t1", "", "game")); err == nil {
		t.Fatal("replace failure not reported")
	}
	replaceFile = old
	assertFailedDurably(t, w, r, root)
}

func TestPublisherFailureIsSurfaced(t *testing.T) {
	w, r, root := openRun(t, runA)
	w.SetPublisher(func(runmodel.Workspace) error { return errors.New("navigation disk full") })
	err := w.Append(r.started())
	var pe *PublishError
	if !errors.As(err, &pe) || !pe.Committed || !errors.Is(err, ErrPublish) || !strings.Contains(err.Error(), "navigation disk full") {
		t.Fatalf("publisher error hidden or misclassified: %v", err)
	}
	// A successful terminal is refused and not journaled; the seq stays free.
	ok := r.finished("success", runmodel.Counts{})
	if err := w.Append(ok); !errors.As(err, &pe) || pe.Committed {
		t.Fatalf("success accepted after publisher failure: %v", err)
	}
	// The writer is not poisoned: an honest error terminal reuses that seq.
	r.seq--
	if err := w.Append(r.finished("error", runmodel.Counts{})); !errors.As(err, &pe) || !pe.Committed {
		t.Fatalf("error terminal not committed: %v", err)
	}
	ws, _ := manifestOnDisk(t, root)
	if ws.Run.Outcome == nil || *ws.Run.Outcome != "error" || ws.LastSeq != 2 {
		t.Fatalf("manifest outcome/lastSeq = %v/%d, want error/2", ws.Run.Outcome, ws.LastSeq)
	}
	assertJournalOutcome(t, root, 2, "error")
}

// A publisher that fails only for the terminal success state must leave no
// durable success: nothing is journaled and the run can record an error.
func TestTerminalPublisherFailureLeavesNoDurableSuccess(t *testing.T) {
	w, r, root := openRun(t, runA)
	failTerminal := false
	w.SetPublisher(func(ws runmodel.Workspace) error {
		if failTerminal {
			return errors.New("index write failed")
		}
		return nil
	})
	c := runmodel.Counts{Total: 1, Success: 1}
	mustAppend(t, w, r.started(), r.discovered("t1", "", "game"), r.targetDone("t1", "", "success"))
	failTerminal = true
	var pe *PublishError
	if err := w.Append(r.finished("success", c)); !errors.As(err, &pe) || pe.Committed {
		t.Fatalf("terminal publisher failure: %v", err)
	}
	ws, _ := manifestOnDisk(t, root)
	if ws.Run.Outcome != nil || ws.LastSeq != 3 {
		t.Fatalf("manifest advanced to outcome %v seq %d after refused success", ws.Run.Outcome, ws.LastSeq)
	}
	assertJournalOutcome(t, root, 3, "")
	r.seq--
	if err := w.Append(r.finished("error", c)); !errors.As(err, &pe) || !pe.Committed {
		t.Fatalf("error terminal: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close after committed error terminal: %v", err)
	}
	ws, _ = manifestOnDisk(t, root)
	if ws.Run.Outcome == nil || *ws.Run.Outcome != "error" || ws.Run.State != "finished" {
		t.Fatalf("manifest run = %+v", ws.Run)
	}
	assertJournalOutcome(t, root, 4, "error")
}

// Navigation for a successful terminal is published before its journal record
// is committed. When that commit then fails, AGENTS.md/INDEX.md must be
// rewritten from the prior projection so they never claim an unjournaled success.
func TestTerminalJournalFailureRewritesNavigation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		fj    *faultyJournal
		patch bool
	}{
		{"write", &faultyJournal{failWrite: 4}, false},
		{"sync", &faultyJournal{failSync: 4}, false},
		{"manifest", &faultyJournal{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withFaultyJournal(t, tc.fj)
			w, r, root := openRun(t, runA)
			var states []string
			w.SetPublisher(func(ws runmodel.Workspace) error {
				states = append(states, ws.Run.State)
				return WriteNavigation(ws)
			})
			c := runmodel.Counts{Total: 1, Success: 1}
			mustAppend(t, w, r.started(), r.discovered("t1", "", "game"), r.targetDone("t1", "", "success"))
			if tc.patch {
				old := replaceFile
				t.Cleanup(func() { replaceFile = old })
				// Navigation files still need the real rename.
				replaceFile = func(src, dst string) error {
					if filepath.Base(dst) == runmodel.ManifestPath {
						return errors.New("simulated manifest replace failure")
					}
					return old(src, dst)
				}
			}
			if err := w.Append(r.finished("success", c)); err == nil {
				t.Fatal("terminal commit failure not reported")
			}
			if len(states) < 2 || states[len(states)-2] != "finished" || states[len(states)-1] == "finished" {
				t.Fatalf("navigation not rewritten after failed success commit: %v", states)
			}
			agents, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(agents), "outcome: **success**") || !strings.Contains(string(agents), "Run state: **running**") {
				t.Fatalf("AGENTS.md still claims success:\n%s", agents)
			}
			_ = w.Close()
			assertJournalOutcome(t, root, 3, "")
		})
	}
}

// assertJournalOutcome checks the journal has exactly n lines and that the
// last run.finished (if want != "") carries outcome want; want "" = none.
func assertJournalOutcome(t *testing.T, root string, n int, want string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(runmodel.JournalPath)))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	if len(lines) != n {
		t.Fatalf("journal lines = %d, want %d", len(lines), n)
	}
	got := ""
	for _, l := range lines {
		var ev runmodel.Event
		if err := json.Unmarshal([]byte(l), &ev); err != nil {
			t.Fatal(err)
		}
		if ev.Type == runmodel.EventRunFinished {
			var d runmodel.RunFinished
			_ = json.Unmarshal(ev.Data, &d)
			got = d.Outcome
		}
	}
	if got != want {
		t.Fatalf("journal terminal outcome = %q, want %q", got, want)
	}
}

func TestProgressIsSyncedAndPublishedWithinInterval(t *testing.T) {
	w, r, root := openRun(t, runA)
	mustAppend(t, w, r.started(), r.discovered("t1", "", "game"))
	for i := 0; i < 50; i++ {
		mustAppend(t, w, r.progress("t1"), r.logLine("x"))
	}
	deadline := time.Now().Add(coalesceInterval + 500*time.Millisecond)
	for {
		ws, _ := manifestOnDisk(t, root)
		if ws.LastSeq == r.seq {
			if ws.Targets[0].State != "running" {
				t.Fatalf("progress did not mark target running: %+v", ws.Targets[0])
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("coalesced progress not published within %v (lastSeq %d of %d)", coalesceInterval, ws.LastSeq, r.seq)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// crash drops the writer like a killed process: no flush, no publish.
func crash(w *Writer) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timer != nil {
		w.timer.Stop()
	}
	w.closed = true
	_ = w.jf.Close()
	_ = w.lock.Close() // the OS drops the lock with the handle
}

func TestCrashAfterAppendBeforeSnapshot(t *testing.T) {
	w, r, root := openRun(t, runA)
	mustAppend(t, w, r.started(), r.discovered("t1", "", "game"), r.progress("t1"))
	crash(w)
	stale, before := manifestOnDisk(t, root)
	if stale.LastSeq != 2 {
		t.Fatalf("stale manifest lastSeq = %d", stale.LastSeq)
	}
	rec, err := Read(root)
	if err != nil {
		t.Fatal(err)
	}
	if rec.LastSeq != 3 || !rec.Interrupted || rec.Active || rec.Workspace.Run.Outcome != nil || rec.Workspace.Targets[0].State != "running" {
		t.Fatalf("recovery = %+v", rec)
	}
	if _, after := manifestOnDisk(t, root); string(after) != string(before) {
		t.Fatal("Read modified the stale manifest")
	}
}

// --- cross-process ---------------------------------------------------------

func TestWorkspaceHelperProcess(t *testing.T) {
	root := os.Getenv("MORGUE_WS_HELPER_ROOT")
	if root == "" {
		t.Skip("helper process only")
	}
	r := newRun(t, root, os.Getenv("MORGUE_WS_HELPER_RUN"))
	w, err := Open(r.h, r.spec)
	switch {
	case errors.Is(err, ErrLocked):
		fmt.Println("busy")
		os.Exit(0)
	case errors.Is(err, ErrNotEmpty):
		fmt.Println("notempty")
		os.Exit(0)
	case err != nil:
		fmt.Println("error", err)
		os.Exit(1)
	}
	mustAppend(t, w, r.started(), r.discovered("t1", "", "game"))
	fmt.Println("locked")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n') // hold until killed
	os.Exit(0)
}

type helper struct {
	cmd *exec.Cmd
	out *bufio.Reader
}

func startHelper(t *testing.T, root, id string) *helper {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestWorkspaceHelperProcess$")
	cmd.Env = append(os.Environ(), "MORGUE_WS_HELPER_ROOT="+root, "MORGUE_WS_HELPER_RUN="+id)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdin.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() })
	return &helper{cmd: cmd, out: bufio.NewReader(stdout)}
}

func (h *helper) line(t *testing.T) string {
	t.Helper()
	s, err := h.out.ReadString('\n')
	if err != nil {
		t.Fatalf("helper output: %q, %v", s, err)
	}
	return strings.TrimSpace(s)
}

func TestCrossProcessLockAndCrashRecovery(t *testing.T) {
	root := filepath.Join(t.TempDir(), "out")
	a := startHelper(t, root, runA)
	if got := a.line(t); got != "locked" {
		t.Fatalf("first process: %q", got)
	}
	b := startHelper(t, root, runB)
	if got := b.line(t); got != "busy" {
		t.Fatalf("second process contending for the same output: %q, want busy", got)
	}
	if rec, err := Read(root); err != nil || !rec.Active || rec.Interrupted {
		t.Fatalf("read while live: %+v, %v", rec, err)
	}
	r := newRun(t, root, runB)
	if _, err := Open(r.h, r.spec); !errors.Is(err, ErrLocked) {
		t.Fatalf("in-process open while helper holds lock: %v", err)
	}
	if _, err := RebuildManifest(root); !errors.Is(err, ErrLocked) {
		t.Fatalf("rebuild while live: %v, want ErrLocked", err)
	}

	if err := a.cmd.Process.Kill(); err != nil { // forced termination
		t.Fatal(err)
	}
	_ = a.cmd.Wait()
	start := time.Now()
	rec, err := Read(root)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Active || !rec.Interrupted || rec.LastSeq != 2 || rec.Workspace.Run.Outcome != nil || len(rec.Workspace.Targets) != 1 {
		t.Fatalf("after kill: %+v", rec)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("inspect took %v", d)
	}
	c := startHelper(t, root, runB)
	if got := c.line(t); got != "notempty" {
		t.Fatalf("rerun into crashed workspace: %q, want notempty (fresh child workspace)", got)
	}
}
