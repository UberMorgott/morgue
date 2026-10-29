package workspace

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/UberMorgott/morgue/internal/runmodel"
)

const (
	morgueDir    = ".morgue"
	lockFileName = "run.lock"
)

// ErrCorrupt marks a journal whose committed interior is unreadable: a
// malformed non-final line, a sequence gap/duplicate or a second run ID.
var ErrCorrupt = errors.New("journal is corrupt")

func journalPath(root string) string {
	return filepath.Join(root, filepath.FromSlash(runmodel.JournalPath))
}

// journalFile is the durable append target; tests substitute faulty storage.
type journalFile interface {
	WriteAt(p []byte, off int64) (int, error)
	Sync() error
	Truncate(size int64) error
	Close() error
}

var createJournal = func(path string) (journalFile, error) {
	return os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
}

// encodeLine renders one journal record: compact Event v2 JSON plus LF.
// encoding/json compacts RawMessage data and escapes control characters, so a
// record never contains a raw newline.
func encodeLine(ev runmodel.Event) ([]byte, error) {
	b, err := json.Marshal(ev)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// journalScan streams a journal and validates its framing. fn receives every
// committed record in order. Validation stops at the first corrupt record;
// records before it have already been delivered (the readable prefix).
type journalScan struct {
	run      string
	lastSeq  uint64
	tornTail bool
	diags    []string
}

func scanJournal(path string, fn func(ev *runmodel.Event) error) (*journalScan, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	s := &journalScan{}
	r := bufio.NewReaderSize(f, 256<<10)
	line := 0
	for {
		raw, rerr := r.ReadSlice('\n')
		if errors.Is(rerr, bufio.ErrBufferFull) {
			// Rare long record (large spec); collect it without bounding.
			full := append([]byte(nil), raw...)
			for errors.Is(rerr, bufio.ErrBufferFull) {
				raw, rerr = r.ReadSlice('\n')
				full = append(full, raw...)
			}
			raw = full
		}
		if rerr != nil && !errors.Is(rerr, io.EOF) {
			return s, fmt.Errorf("read journal: %w", rerr)
		}
		if len(raw) == 0 && errors.Is(rerr, io.EOF) {
			return s, nil
		}
		line++
		if errors.Is(rerr, io.EOF) {
			// Unterminated final line: an interrupted or in-progress append.
			// It was never synced as a whole record, so it is not committed.
			s.tornTail = true
			s.diags = append(s.diags, fmt.Sprintf("journal line %d is incomplete (torn final record, %d bytes ignored)", line, len(raw)))
			return s, nil
		}
		var ev runmodel.Event
		if err := json.Unmarshal(bytes.TrimSuffix(raw, []byte{'\n'}), &ev); err != nil {
			return s, fmt.Errorf("%w: line %d: malformed record: %v", ErrCorrupt, line, err)
		}
		if ev.Version != runmodel.EventVersion {
			return s, fmt.Errorf("journal line %d: unsupported event version %d (this build reads v%d)", line, ev.Version, runmodel.EventVersion)
		}
		if s.run == "" {
			if ev.Type != runmodel.EventRunStarted {
				return s, fmt.Errorf("%w: line %d: first record is %q, want run.started", ErrCorrupt, line, ev.Type)
			}
			s.run = ev.Run
		} else if ev.Run != s.run {
			return s, fmt.Errorf("%w: line %d: second run ID %q in journal of run %q", ErrCorrupt, line, ev.Run, s.run)
		}
		if ev.Seq != s.lastSeq+1 {
			return s, fmt.Errorf("%w: line %d: sequence %d after %d (gap or duplicate)", ErrCorrupt, line, ev.Seq, s.lastSeq)
		}
		if err := fn(&ev); err != nil {
			return s, err
		}
		s.lastSeq = ev.Seq
	}
}
