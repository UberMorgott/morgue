package workspace

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/UberMorgott/morgue/internal/runmodel"
)

// coalesceInterval bounds how long a progress/log record may stay unsynced and
// how often progress-only snapshots are published.
const coalesceInterval = time.Second

// Writer is the single durable writer of one run workspace. Every record is
// appended to the journal before the manifest snapshot derived from it is
// atomically replaced; the manifest's lastSeq never exceeds the synced prefix.
type Writer struct {
	mu        sync.Mutex
	root      string
	handle    runmodel.RunHandle
	spec      runmodel.RunSpec
	lock      *os.File
	jf        journalFile
	size      int64 // committed journal length
	lastSeq   uint64
	syncedSeq uint64 // journal Sync covers records through this seq
	pubSeq    uint64 // morgue.json reflects records through this seq
	lastPub   time.Time
	proj      *projector
	publisher func(runmodel.Workspace) error
	timer     *time.Timer
	failed    error // sticky durability error
	navErr    error // sticky navigation publisher error (journal/manifest stay valid)
	closed    bool
}

var _ Sink = (*Writer)(nil)

// ErrPublish marks a navigation publisher failure (errors.Is target).
var ErrPublish = errors.New("publish navigation")

// PublishError reports a navigation publisher failure. It does not poison the
// Writer: journal and manifest stay valid and later records are still accepted,
// so the run can record an honest error outcome. Committed tells whether the
// event itself reached the journal and manifest. A successful run.finished is
// never committed after any publisher failure: navigation for a successful
// terminal state is published before the record is journaled, so a failure can
// never leave a durable success behind.
type PublishError struct {
	Seq       uint64
	Committed bool
	Err       error
}

func (e *PublishError) Error() string {
	return fmt.Sprintf("publish navigation (event %d): %v", e.Seq, e.Err)
}

// Unwrap exposes both ErrPublish and the publisher's own error.
func (e *PublishError) Unwrap() []error { return []error{ErrPublish, e.Err} }

// successfulTerminal reports whether ev is run.finished with outcome success.
func successfulTerminal(ev *runmodel.Event) bool {
	if ev.Type != runmodel.EventRunFinished {
		return false
	}
	var d runmodel.RunFinished
	return json.Unmarshal(ev.Data, &d) == nil && d.Outcome == runmodel.OutcomeSuccess
}

// Open admits a fresh workspace at handle.Output: it takes the OS lifetime
// lock in .morgue/run.lock (ErrLocked wins over the emptiness check), accepts
// only an empty directory (ErrNotEmpty otherwise, leaving it untouched), and
// creates the journal and the provisional run tool lock. No manifest is
// published until the first event.
func Open(handle runmodel.RunHandle, spec runmodel.RunSpec) (*Writer, error) {
	if handle.ID == "" || handle.Output == "" {
		return nil, fmt.Errorf("workspace: handle needs run id and output")
	}
	root, err := filepath.Abs(handle.Output)
	if err != nil {
		return nil, err
	}
	createdRoot := false
	if _, err := os.Stat(root); errors.Is(err, fs.ErrNotExist) {
		createdRoot = true
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("workspace: create output: %w", err)
	}
	meta := filepath.Join(root, morgueDir)
	createdMeta := false
	if _, err := os.Lstat(meta); errors.Is(err, fs.ErrNotExist) {
		createdMeta = true
	}
	if err := os.MkdirAll(meta, 0o755); err != nil {
		return nil, fmt.Errorf("workspace: create %s: %w", morgueDir, err)
	}
	lockPath := filepath.Join(meta, lockFileName)
	_, statErr := os.Lstat(lockPath)
	createdLock := errors.Is(statErr, fs.ErrNotExist)
	lock, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("workspace: open lock: %w", err)
	}
	if err := lockFile(lock); err != nil {
		_ = lock.Close()
		if errors.Is(err, ErrLocked) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("workspace: lock: %w", err)
	}
	w := &Writer{root: root, handle: handle, spec: spec, lock: lock, proj: newProjector(true)}
	abandon := func() {
		_ = unlockFile(lock)
		_ = lock.Close()
		if createdLock {
			_ = os.Remove(lockPath)
		}
		if createdMeta {
			_ = os.Remove(meta) // only succeeds while empty
		}
		if createdRoot {
			_ = os.Remove(root)
		}
	}
	empty, err := emptyWorkspace(root)
	if err != nil {
		abandon()
		return nil, fmt.Errorf("workspace: inspect output: %w", err)
	}
	if !empty {
		abandon()
		return nil, ErrNotEmpty
	}
	tools, _ := json.Marshal(struct {
		SchemaVersion int        `json:"schemaVersion"`
		Complete      bool       `json:"complete"`
		Tools         []struct{} `json:"tools"`
	}{1, false, []struct{}{}})
	if err := writeAtomic(filepath.Join(root, filepath.FromSlash(runmodel.RunToolsLockPath)), append(tools, '\n')); err != nil {
		abandon()
		return nil, fmt.Errorf("workspace: write tools lock: %w", err)
	}
	jf, err := createJournal(journalPath(root))
	if err != nil {
		abandon()
		return nil, fmt.Errorf("workspace: create journal: %w", err)
	}
	w.jf = jf
	return w, nil
}

// emptyWorkspace ignores only lock scaffolding: .morgue holding nothing but
// run.lock.
func emptyWorkspace(root string) (bool, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.Name() != morgueDir {
			return false, nil
		}
		if !e.IsDir() {
			return false, nil
		}
		inner, err := os.ReadDir(filepath.Join(root, morgueDir))
		if err != nil {
			return false, err
		}
		for _, ie := range inner {
			if ie.Name() != lockFileName {
				return false, nil
			}
		}
	}
	return true, nil
}

// Root returns the absolute workspace directory.
func (w *Writer) Root() string { return w.root }

// SetPublisher installs the navigation callback invoked with every published
// snapshot (after morgue.json is replaced). Install it before the first event.
func (w *Writer) SetPublisher(fn func(runmodel.Workspace) error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.publisher = fn
}

// Append durably records ev. Lifecycle, target, artifact and quality records
// are synced and published before Append returns; progress/log records are
// synced and published at most coalesceInterval later. On any error the
// journal is cut back to its previous boundary where storage permits, the
// snapshot does not advance and the writer stays failed. A navigation
// publisher failure is the exception: it returns *PublishError (see there)
// without poisoning the writer.
func (w *Writer) Append(ev runmodel.Event) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errors.New("workspace: writer closed")
	}
	if w.failed != nil {
		return fmt.Errorf("workspace: earlier durability failure: %w", w.failed)
	}
	if err := w.validate(&ev); err != nil {
		return err // contract violation; nothing written, writer still usable
	}
	line, err := encodeLine(ev)
	if err != nil {
		return fmt.Errorf("workspace: encode event %d: %w", ev.Seq, err)
	}
	immediate := publishesNow(ev.Type)
	success := successfulTerminal(&ev)
	if success && w.navErr != nil {
		return &PublishError{Seq: ev.Seq, Err: fmt.Errorf("success refused after earlier failure: %w", w.navErr)}
	}
	undo := w.proj.checkpoint(&ev, immediate)
	if err := w.proj.apply(&ev); err != nil {
		undo()
		return fmt.Errorf("workspace: %w", err)
	}
	if success {
		// Publish the successful terminal navigation before committing the
		// record: on failure nothing is journaled and the caller can record an
		// error outcome instead of a durable success.
		if err := w.notifyLocked(); err != nil {
			undo()
			w.navErr = err
			return &PublishError{Seq: ev.Seq, Err: err}
		}
	}
	prevSize := w.size
	rollback := func(cause error) error {
		undo()
		if terr := w.jf.Truncate(prevSize); terr == nil {
			_ = w.jf.Sync()
		}
		w.fail(cause)
		if success {
			// Navigation was published for the success projection before the
			// record was committed. The commit failed, so republish the prior
			// (unfinished) projection: AGENTS.md/INDEX.md must not claim a
			// success the journal does not hold.
			if nerr := w.notifyLocked(); nerr != nil {
				w.navErr = nerr
				return fmt.Errorf("workspace: %w (navigation still claims success: %w)", cause, nerr)
			}
		}
		return fmt.Errorf("workspace: %w", cause)
	}
	if _, err := w.jf.WriteAt(line, w.size); err != nil {
		return rollback(fmt.Errorf("append event %d: %w", ev.Seq, err))
	}
	w.size += int64(len(line))
	w.lastSeq = ev.Seq
	if immediate || time.Since(w.lastPub) >= coalesceInterval {
		if err := w.syncLocked(); err != nil {
			w.size, w.lastSeq = prevSize, ev.Seq-1
			return rollback(err)
		}
		if err := w.writeManifestLocked(); err != nil {
			w.size, w.lastSeq, w.syncedSeq = prevSize, ev.Seq-1, ev.Seq-1
			return rollback(err)
		}
		if success {
			return nil // navigation already published for this exact projection
		}
		// The manifest now covers ev; a publisher failure cannot be rolled
		// back without making the manifest run ahead of the journal.
		if err := w.notifyLocked(); err != nil {
			w.navErr = err
			return &PublishError{Seq: ev.Seq, Committed: true, Err: err}
		}
		return nil
	}
	w.scheduleLocked()
	return nil
}

// validate checks the envelope against this run: version, run identity,
// contiguous sequence and a single started/finished lifecycle.
func (w *Writer) validate(ev *runmodel.Event) error {
	if ev.Version != runmodel.EventVersion {
		return fmt.Errorf("workspace: unsupported event version %d", ev.Version)
	}
	if ev.Run != w.handle.ID {
		return fmt.Errorf("workspace: event for run %q in workspace of run %q", ev.Run, w.handle.ID)
	}
	if ev.Seq != w.lastSeq+1 {
		return fmt.Errorf("workspace: event seq %d, want %d", ev.Seq, w.lastSeq+1)
	}
	if ev.TS.IsZero() || len(ev.Data) == 0 {
		return fmt.Errorf("workspace: event %d lacks ts or data", ev.Seq)
	}
	if w.lastSeq == 0 {
		if ev.Type != runmodel.EventRunStarted {
			return fmt.Errorf("workspace: first event is %q, want run.started", ev.Type)
		}
		var d runmodel.RunStarted
		if err := json.Unmarshal(ev.Data, &d); err != nil {
			return fmt.Errorf("workspace: run.started payload: %w", err)
		}
		if d.Handle.ID != w.handle.ID || !d.Handle.StartedAt.Equal(w.handle.StartedAt) || filepath.Clean(d.Handle.Output) != filepath.Clean(w.handle.Output) {
			return fmt.Errorf("workspace: run.started handle does not match admitted handle")
		}
		got, _ := json.Marshal(d.Spec)
		want, _ := json.Marshal(w.spec)
		if !bytes.Equal(got, want) {
			return fmt.Errorf("workspace: run.started spec does not match admitted spec")
		}
	}
	return nil
}

func publishesNow(t runmodel.EventType) bool {
	switch t {
	case runmodel.EventRunStarted, runmodel.EventTargetDiscovered, runmodel.EventTargetFinished,
		runmodel.EventArtifactWritten, runmodel.EventQualityUpdated, runmodel.EventRunFinished:
		return true
	case runmodel.EventScanCompleted, runmodel.EventStepProgress, runmodel.EventToolProgress, runmodel.EventLog:
		// progress: coalesced, published on the next tick
	}
	return false
}

func (w *Writer) fail(err error) {
	if w.failed == nil {
		w.failed = err
	}
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
}

func (w *Writer) syncLocked() error {
	if w.syncedSeq == w.lastSeq {
		return nil
	}
	if err := w.jf.Sync(); err != nil {
		return fmt.Errorf("sync journal: %w", err)
	}
	w.syncedSeq = w.lastSeq
	return nil
}

func (w *Writer) writeManifestLocked() error {
	ws := w.proj.clone()
	if ws.LastSeq > w.syncedSeq {
		return fmt.Errorf("manifest seq %d ahead of synced journal %d", ws.LastSeq, w.syncedSeq)
	}
	data, err := encodeManifest(ws)
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	if err := writeAtomic(filepath.Join(w.root, runmodel.ManifestPath), data); err != nil {
		return fmt.Errorf("publish manifest: %w", err)
	}
	w.pubSeq = ws.LastSeq
	w.lastPub = time.Now()
	return nil
}

func (w *Writer) notifyLocked() error {
	if w.publisher == nil {
		return nil
	}
	return w.publisher(w.proj.clone())
}

// scheduleLocked arranges the coalesced sync+publish of pending progress.
func (w *Writer) scheduleLocked() {
	if w.timer != nil {
		return
	}
	wait := max(coalesceInterval-time.Since(w.lastPub), 0)
	w.timer = time.AfterFunc(wait, w.flushTick)
}

func (w *Writer) flushTick() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.timer = nil
	if w.closed || w.failed != nil {
		return
	}
	if err := w.flushLocked(); err != nil && !errors.Is(err, ErrPublish) {
		w.fail(err)
	}
}

// flushLocked syncs and publishes records not yet covered by morgue.json. A
// publisher failure is returned as a committed *PublishError (navErr recorded,
// writer not poisoned); sync/manifest failures are durability errors.
func (w *Writer) flushLocked() error {
	if w.pubSeq == w.lastSeq {
		return nil
	}
	if err := w.syncLocked(); err != nil {
		return err
	}
	if err := w.writeManifestLocked(); err != nil {
		return err
	}
	if err := w.notifyLocked(); err != nil {
		w.navErr = err
		return &PublishError{Seq: w.pubSeq, Committed: true, Err: err}
	}
	return nil
}

// Snapshot returns the projection through the last appended record, syncing
// the journal first so its lastSeq is durable. ErrNoEvents before run.started.
func (w *Writer) Snapshot() (runmodel.Workspace, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.proj.started {
		return runmodel.Workspace{}, ErrNoEvents
	}
	if w.failed != nil {
		return runmodel.Workspace{}, fmt.Errorf("workspace: earlier durability failure: %w", w.failed)
	}
	if !w.closed {
		if err := w.syncLocked(); err != nil {
			w.fail(err)
			return runmodel.Workspace{}, fmt.Errorf("workspace: %w", err)
		}
	}
	return w.proj.clone(), nil
}

// Close publishes any coalesced progress, closes the journal and releases the
// OS lock, even after a durability failure. It returns the flush/close error,
// or the earlier durability failure so it cannot be mistaken for success.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
	var err error
	if w.failed == nil && w.proj.started {
		if ferr := w.flushLocked(); errors.Is(ferr, ErrPublish) {
			err = fmt.Errorf("workspace: %w", ferr)
		} else if ferr != nil {
			w.fail(ferr)
		}
	}
	if w.failed != nil {
		err = fmt.Errorf("workspace: %w", w.failed)
	}
	w.closed = true
	if cerr := w.jf.Close(); err == nil && cerr != nil {
		err = fmt.Errorf("workspace: close journal: %w", cerr)
	}
	_ = unlockFile(w.lock)
	if cerr := w.lock.Close(); err == nil && cerr != nil {
		err = fmt.Errorf("workspace: release lock: %w", cerr)
	}
	return err
}
