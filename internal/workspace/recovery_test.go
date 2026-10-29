package workspace

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/UberMorgott/morgue/internal/runmodel"
)

// completedWorkspace writes a finished run and returns its root and journal.
func completedWorkspace(t *testing.T) (string, []byte) {
	t.Helper()
	w, r, root := openRun(t, runA)
	mustAppend(t, w, r.started(), r.discovered("t1", "", "game"), r.progress("t1"), r.targetDone("t1", "", "success"),
		r.finished("success", runmodel.Counts{Total: 1, Success: 1}))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(journalPath(root))
	if err != nil {
		t.Fatal(err)
	}
	return root, b
}

func setJournal(t *testing.T, root string, lines [][]byte, tail string) {
	t.Helper()
	data := append(bytes.Join(lines, []byte("\n")), '\n')
	data = append(data, tail...)
	if err := os.WriteFile(journalPath(root), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func splitLines(b []byte) [][]byte {
	return bytes.Split(bytes.TrimSuffix(b, []byte("\n")), []byte("\n"))
}

func TestReadIgnoresOnlyTornFinalLine(t *testing.T) {
	root, j := completedWorkspace(t)
	lines := splitLines(j)
	// Drop run.finished and leave half of it as an unterminated tail.
	last := lines[len(lines)-1]
	setJournal(t, root, lines[:len(lines)-1], string(last[:len(last)/2]))
	rec, err := Read(root)
	if err != nil {
		t.Fatal(err)
	}
	if !rec.TornTail || rec.LastSeq != 4 || !rec.Interrupted || rec.Workspace.Run.Outcome != nil {
		t.Fatalf("torn tail: %+v", rec)
	}
	if !strings.Contains(strings.Join(rec.Diagnostics, "\n"), "torn final record") {
		t.Fatalf("no torn-tail diagnostic: %v", rec.Diagnostics)
	}
	// A complete JSON record without its LF is still not committed.
	setJournal(t, root, lines[:len(lines)-1], string(last))
	if rec, err := Read(root); err != nil || !rec.TornTail || rec.LastSeq != 4 {
		t.Fatalf("unterminated complete record: %+v, %v", rec, err)
	}
}

func TestReadRejectsInteriorCorruptionWithPrefix(t *testing.T) {
	root, j := completedWorkspace(t)
	lines := splitLines(j)
	cases := map[string][][]byte{
		"malformed": {lines[0], lines[1], []byte(`{"v":2,"ts":`), lines[3], lines[4]},
		"gap":       {lines[0], lines[1], lines[3], lines[4]},
		"duplicate": {lines[0], lines[1], lines[1], lines[2]},
		"other-run": {lines[0], lines[1], bytes.Replace(lines[2], []byte(runA), []byte(runB), 1)},
	}
	for name, ls := range cases {
		t.Run(name, func(t *testing.T) {
			setJournal(t, root, ls, "")
			rec, err := Read(root)
			if !errors.Is(err, ErrCorrupt) {
				t.Fatalf("err = %v, want ErrCorrupt", err)
			}
			if rec.LastSeq != 2 || rec.Workspace.Run.ID != runA || len(rec.Workspace.Targets) != 1 {
				t.Fatalf("prefix not exposed: %+v", rec)
			}
		})
	}
}

func TestReadVersionAndUnknownTypes(t *testing.T) {
	root, j := completedWorkspace(t)
	lines := splitLines(j)
	future := []byte(fmt.Sprintf(`{"v":2,"ts":"2026-10-29T01:02:03.5Z","seq":5,"run":%q,"type":"future.thing","data":{"x":1},"extra":true}`, runA))
	setJournal(t, root, append(append([][]byte{}, lines[:4]...), future), "")
	rec, err := Read(root)
	if err != nil || rec.LastSeq != 5 {
		t.Fatalf("unknown type must be retained and skipped: %+v, %v", rec, err)
	}
	v3 := bytes.Replace(lines[2], []byte(`"v":2`), []byte(`"v":3`), 1)
	setJournal(t, root, [][]byte{lines[0], lines[1], v3}, "")
	if _, err := Read(root); err == nil || !strings.Contains(err.Error(), "unsupported event version 3") {
		t.Fatalf("unsupported version: %v", err)
	}
	if err := os.Remove(journalPath(root)); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(root); !errors.Is(err, ErrNoEvents) {
		t.Fatalf("missing journal: %v", err)
	}
}

func TestReadEventsPagesWithoutBuffering(t *testing.T) {
	w, r, root := openRun(t, runA)
	mustAppend(t, w, r.started())
	for i := 0; i < 2499; i++ {
		mustAppend(t, w, r.logLine("line"))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	var after uint64
	var got int
	for pages := 0; ; pages++ {
		p, err := ReadEvents(root, runA, after, 5000)
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Events) > maxEventPage || p.LastSeq != 2500 || p.RunID != runA {
			t.Fatalf("page: n=%d lastSeq=%d", len(p.Events), p.LastSeq)
		}
		for _, ev := range p.Events {
			if ev.Seq != after+1 {
				t.Fatalf("page seq %d after %d", ev.Seq, after)
			}
			after = ev.Seq
			got++
		}
		if !p.HasMore {
			break
		}
		if pages > 3 {
			t.Fatal("too many pages")
		}
	}
	if got != 2500 {
		t.Fatalf("replayed %d events", got)
	}
	if p, err := ReadEvents(root, runA, 2500, 10); err != nil || len(p.Events) != 0 || p.HasMore {
		t.Fatalf("caught-up page: %+v, %v", p, err)
	}
	if _, err := ReadEvents(root, runB, 0, 10); !errors.Is(err, ErrRunMismatch) {
		t.Fatalf("old run id: %v", err)
	}
	// An actively appended incomplete final line is tolerated.
	f, _ := os.OpenFile(journalPath(root), os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString(`{"v":2,"seq":2501`)
	_ = f.Close()
	if p, err := ReadEvents(root, runA, 2490, 1000); err != nil || len(p.Events) != 10 || p.LastSeq != 2500 {
		t.Fatalf("with torn tail: n=%d lastSeq=%d err=%v", len(p.Events), p.LastSeq, err)
	}
}

func TestRebuildManifestNeedsLockAndPreservesPrior(t *testing.T) {
	w, r, root := openRun(t, runA)
	mustAppend(t, w, r.started(), r.discovered("t1", "", "game"), r.progress("t1"))
	if _, err := RebuildManifest(root); !errors.Is(err, ErrLocked) {
		t.Fatalf("rebuild with live writer: %v", err)
	}
	crash(w)
	_, stale := manifestOnDisk(t, root)
	rec, err := RebuildManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	ws, _ := manifestOnDisk(t, root)
	if ws.LastSeq != 3 || rec.LastSeq != 3 || !rec.Interrupted || ws.Run.Outcome != nil {
		t.Fatalf("rebuilt manifest: lastSeq=%d rec=%+v", ws.LastSeq, rec)
	}
	if !strings.Contains(strings.Join(ws.Warnings, "\n"), "interrupted") {
		t.Fatalf("rebuild warnings: %v", ws.Warnings)
	}
	prev, _ := filepath.Glob(filepath.Join(root, morgueDir, "morgue.*.prev.json"))
	if len(prev) != 1 {
		t.Fatalf("prior snapshot not preserved: %v", prev)
	}
	if b, _ := os.ReadFile(prev[0]); !bytes.Equal(b, stale) {
		t.Fatal("preserved snapshot differs from the prior manifest")
	}
	// Corrupt interior: refuse to rebuild, keep the manifest.
	j, _ := os.ReadFile(journalPath(root))
	lines := splitLines(j)
	setJournal(t, root, [][]byte{lines[0], lines[2]}, "")
	_, before := manifestOnDisk(t, root)
	if _, err := RebuildManifest(root); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("rebuild of corrupt journal: %v", err)
	}
	if _, after := manifestOnDisk(t, root); !bytes.Equal(before, after) {
		t.Fatal("corrupt rebuild touched the manifest")
	}
}

func raceEnabled() bool {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "-race" {
				return s.Value == "true"
			}
		}
	}
	return false
}

// M2 acceptance: a 100k-event synthetic journal of a forcibly terminated run
// is inspected within 2 s with < 128 MiB incremental streaming memory.
func TestRead100kEventJournalBounded(t *testing.T) {
	root := filepath.Join(t.TempDir(), "out")
	r := newRun(t, root, runA)
	if err := os.MkdirAll(filepath.Join(root, morgueDir), 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	add := func(ev runmodel.Event) {
		line, err := encodeLine(ev)
		if err != nil {
			t.Fatal(err)
		}
		buf.Write(line)
	}
	add(r.started())
	for i := 0; i < 200; i++ {
		add(r.discovered(fmt.Sprintf("t%04d", i), "", fmt.Sprintf("game/t%04d", i)))
	}
	for r.seq < 100_000 {
		if r.seq%2 == 0 {
			add(r.progress(fmt.Sprintf("t%04d", r.seq%200)))
		} else {
			add(r.logLine("decompiling some function with a moderately long log message"))
		}
	}
	if err := os.WriteFile(journalPath(root), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	buf = bytes.Buffer{}
	runtime.GC()
	var base runtime.MemStats
	runtime.ReadMemStats(&base)
	var peak atomic.Uint64
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		var m runtime.MemStats
		for {
			runtime.ReadMemStats(&m)
			if m.HeapAlloc > peak.Load() {
				peak.Store(m.HeapAlloc)
			}
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Millisecond):
			}
		}
	}()
	start := time.Now()
	rec, err := Read(root)
	elapsed := time.Since(start)
	close(stop)
	wg.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if rec.LastSeq != 100_000 || !rec.Interrupted || len(rec.Workspace.Targets) != 200 {
		t.Fatalf("recovery: lastSeq=%d interrupted=%v targets=%d", rec.LastSeq, rec.Interrupted, len(rec.Workspace.Targets))
	}
	var incr uint64
	if p := peak.Load(); p > base.HeapAlloc {
		incr = p - base.HeapAlloc
	}
	t.Logf("100k events: %v, peak incremental heap %.1f MiB", elapsed, float64(incr)/(1<<20))
	if incr > 128<<20 {
		t.Fatalf("incremental heap %d bytes exceeds 128 MiB", incr)
	}
	if elapsed > 2*time.Second && !raceEnabled() {
		t.Fatalf("inspect took %v, want <= 2s", elapsed)
	}
}
