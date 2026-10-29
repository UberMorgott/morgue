package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/UberMorgott/morgue/internal/runmodel"
)

const maxEventPage = 1000

// ErrRunMismatch: the workspace journal belongs to a different run ID.
var ErrRunMismatch = errors.New("workspace belongs to a different run")

// lockActive reports whether a live process holds the workspace OS lock. It
// never creates the lock file, so reading stays non-mutating.
func lockActive(root string) (bool, error) {
	f, err := os.OpenFile(filepath.Join(root, morgueDir, lockFileName), os.O_RDWR, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	if err := lockFile(f); err != nil {
		if errors.Is(err, ErrLocked) {
			return true, nil
		}
		return false, err
	}
	_ = unlockFile(f)
	return false, nil
}

// Read projects the committed journal prefix without modifying anything. A
// torn final line is ignored with a diagnostic. Interior corruption, sequence
// gaps/duplicates and foreign run IDs return an error together with the
// recovery of the readable prefix. A run without run.finished and without a
// live lock is Interrupted; no cancellation is fabricated.
func Read(root string) (Recovery, error) {
	active, err := lockActive(root)
	if err != nil {
		return Recovery{}, fmt.Errorf("workspace: probe lock: %w", err)
	}
	return read(root, active)
}

func read(root string, active bool) (Recovery, error) {
	rec := Recovery{Active: active, Diagnostics: []string{}}
	p := newProjector(false)
	scan, err := scanJournal(journalPath(root), func(ev *runmodel.Event) error {
		if err := p.apply(ev); err != nil {
			return fmt.Errorf("%w: %v", ErrCorrupt, err)
		}
		return nil
	})
	if scan != nil {
		rec.LastSeq = scan.lastSeq
		rec.TornTail = scan.tornTail
		rec.Diagnostics = append(rec.Diagnostics, scan.diags...)
	}
	rec.Diagnostics = append(rec.Diagnostics, p.diags...)
	if errors.Is(err, fs.ErrNotExist) {
		return rec, fmt.Errorf("workspace: %s has no journal: %w", root, ErrNoEvents)
	}
	if rec.LastSeq == 0 {
		if err == nil {
			err = ErrNoEvents
		}
		return rec, fmt.Errorf("workspace: %w", err)
	}
	rec.Workspace = p.clone()
	rec.Workspace.LastSeq = rec.LastSeq
	rec.Interrupted = !p.finished && !active
	if rec.Interrupted {
		rec.Diagnostics = append(rec.Diagnostics, fmt.Sprintf("run %s is interrupted: no run.finished after seq %d and no live writer", rec.Workspace.Run.ID, rec.LastSeq))
	}
	if err != nil {
		rec.Diagnostics = append(rec.Diagnostics, err.Error())
		return rec, fmt.Errorf("workspace: %w", err)
	}
	return rec, nil
}

// ReadEvents returns up to limit (1..1000) committed records with seq > after,
// streaming the journal without buffering it. LastSeq is the journal's last
// committed seq; HasMore reports records beyond this page.
func ReadEvents(root, runID string, after uint64, limit int) (EventPage, error) {
	if limit <= 0 || limit > maxEventPage {
		limit = maxEventPage
	}
	page := EventPage{RunID: runID, Events: []runmodel.Event{}}
	scan, err := scanJournal(journalPath(root), func(ev *runmodel.Event) error {
		if ev.Seq == 1 && ev.Run != runID {
			return fmt.Errorf("%w: journal run %q, requested %q", ErrRunMismatch, ev.Run, runID)
		}
		if ev.Seq > after && len(page.Events) < limit {
			page.Events = append(page.Events, *ev)
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return page, fmt.Errorf("workspace: %s has no journal: %w", root, ErrNoEvents)
	}
	if scan != nil {
		page.LastSeq = scan.lastSeq
	}
	if err != nil {
		return page, fmt.Errorf("workspace: %w", err)
	}
	if page.LastSeq == 0 {
		return page, fmt.Errorf("workspace: %w", ErrNoEvents)
	}
	if n := len(page.Events); n > 0 {
		page.HasMore = page.Events[n-1].Seq < page.LastSeq
	} else {
		page.HasMore = false
	}
	return page, nil
}

// RebuildManifest re-projects morgue.json from the committed journal prefix.
// It takes the exclusive workspace lock (ErrLocked while a writer is live),
// keeps the prior snapshot as .morgue/morgue.<UTC>.prev.json, and refuses a
// journal with interior corruption.
func RebuildManifest(root string) (Recovery, error) {
	meta := filepath.Join(root, morgueDir)
	if _, err := os.Stat(journalPath(root)); err != nil {
		return Recovery{}, fmt.Errorf("workspace: %s has no journal: %w", root, ErrNoEvents)
	}
	lock, err := os.OpenFile(filepath.Join(meta, lockFileName), os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return Recovery{}, fmt.Errorf("workspace: open lock: %w", err)
	}
	defer func() { _ = lock.Close() }()
	if err := lockFile(lock); err != nil {
		if errors.Is(err, ErrLocked) {
			return Recovery{}, ErrLocked
		}
		return Recovery{}, fmt.Errorf("workspace: lock: %w", err)
	}
	defer func() { _ = unlockFile(lock) }()
	rec, err := read(root, false)
	if err != nil {
		return rec, err
	}
	manifest := filepath.Join(root, runmodel.ManifestPath)
	if prev, err := os.ReadFile(manifest); err == nil {
		name := "morgue." + time.Now().UTC().Format("20060102T150405.000000000Z") + ".prev.json"
		if err := writeAtomic(filepath.Join(meta, name), prev); err != nil {
			return rec, fmt.Errorf("workspace: preserve prior manifest: %w", err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return rec, fmt.Errorf("workspace: read prior manifest: %w", err)
	}
	ws := rec.Workspace
	ws.Warnings = append(append([]string{}, ws.Warnings...), fmt.Sprintf("manifest rebuilt from journal through seq %d at %s", rec.LastSeq, time.Now().UTC().Format(time.RFC3339)))
	if rec.Interrupted {
		ws.Warnings = append(ws.Warnings, "run interrupted: the journal has no run.finished; state is the committed prefix, not a completed run")
	}
	data, err := encodeManifest(ws)
	if err != nil {
		return rec, err
	}
	if err := writeAtomic(manifest, data); err != nil {
		return rec, fmt.Errorf("workspace: publish rebuilt manifest: %w", err)
	}
	rec.Workspace = ws
	return rec, nil
}
