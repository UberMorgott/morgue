package services

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/UberMorgott/morgue/internal/runmodel"
	"github.com/UberMorgott/morgue/internal/workspace"
)

// ReplayPageSize bounds every events replay page (HTTP and Wails).
const ReplayPageSize = 1000

var (
	// ErrNoCurrentRun: no run has been admitted in this GUI session.
	ErrNoCurrentRun = errors.New("no current run")
	// ErrStaleRun: the requested run ID is not the current run.
	ErrStaleRun = errors.New("run is not the current run")
)

// WorkspaceReader is the read-only persistence surface the service needs
// (workspace.Read / workspace.ReadEvents in production, fakes in tests).
type WorkspaceReader struct {
	Read       func(root string) (workspace.Recovery, error)
	ReadEvents func(root, runID string, after uint64, limit int) (workspace.EventPage, error)
}

// RecentRun is one recent workspace reference with status resolved from disk.
type RecentRun struct {
	Path      string     `json:"path"`
	Input     string     `json:"input"`
	RunID     string     `json:"runId"`
	Status    string     `json:"status"` // running|finished|interrupted|corrupt|legacy|unavailable
	Outcome   string     `json:"outcome"`
	StartedAt *time.Time `json:"startedAt"`
	Targets   int64      `json:"targets"`
	Failed    int64      `json:"failed"`
	Error     string     `json:"error"`
}

// CurrentRun is the snapshot of the run admitted most recently in this process.
type CurrentRun struct {
	RunID    string             `json:"runId"`
	Root     string             `json:"root"`
	Recovery workspace.Recovery `json:"recovery"`
}

// WorkspaceService exposes read-only workspace snapshots, bounded replay and
// recent workspace references to Wails and the HTTP API.
type WorkspaceService struct {
	reader WorkspaceReader
	recent *workspace.RecentStore

	mu      sync.Mutex
	runID   string
	root    string
	lastSeq uint64
}

// NewWorkspaceService builds the service. recent nil uses the store that
// WriteNavigation records into.
func NewWorkspaceService(reader WorkspaceReader, recent *workspace.RecentStore) *WorkspaceService {
	if recent == nil {
		recent = workspace.DefaultRecentStore()
	}
	return &WorkspaceService{reader: reader, recent: recent}
}

// ObserveWorkspaceEvent tracks the current run root from delivered events.
// Package function so the Wails binding generator does not expose it.
func ObserveWorkspaceEvent(s *WorkspaceService, ev runmodel.Event) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ev.Type == runmodel.EventRunStarted {
		var p runmodel.RunStarted
		if json.Unmarshal(ev.Data, &p) == nil && p.Handle.Output != "" {
			s.runID, s.root, s.lastSeq = ev.Run, p.Handle.Output, ev.Seq
		}
		return
	}
	if ev.Run == s.runID && ev.Seq > s.lastSeq {
		s.lastSeq = ev.Seq
	}
}

func (s *WorkspaceService) current() (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runID, s.root
}

// Current returns the on-disk snapshot of the current run's workspace.
func (s *WorkspaceService) Current() (CurrentRun, error) {
	runID, root := s.current()
	if runID == "" {
		return CurrentRun{}, ErrNoCurrentRun
	}
	rec, err := s.read(root)
	return CurrentRun{RunID: runID, Root: root, Recovery: rec}, err
}

// Events returns up to ReplayPageSize journal events of runID after seq.
func (s *WorkspaceService) Events(runID string, after uint64) (workspace.EventPage, error) {
	return s.EventsLimit(runID, after, ReplayPageSize)
}

// EventsLimit is Events with an explicit page size clamped to 1..ReplayPageSize.
func (s *WorkspaceService) EventsLimit(runID string, after uint64, limit int) (workspace.EventPage, error) {
	cur, root := s.current()
	if cur == "" {
		return workspace.EventPage{}, ErrNoCurrentRun
	}
	if runID != cur {
		return workspace.EventPage{}, ErrStaleRun
	}
	if limit <= 0 || limit > ReplayPageSize {
		limit = ReplayPageSize
	}
	if s.reader.ReadEvents == nil {
		return workspace.EventPage{}, errors.New("workspace replay unavailable")
	}
	page, err := s.reader.ReadEvents(root, runID, after, limit)
	if page.Events == nil {
		page.Events = []runmodel.Event{}
	}
	return page, err
}

// ReplayEvents streams every event of runID after seq, page by page, to emit.
// Used by SSE Last-Event-ID reconnect; never buffers the whole journal. A package
// function so the Wails binding generator does not expose a func parameter.
func ReplayEvents(s *WorkspaceService, runID string, after uint64, emit func(runmodel.Event) error) (uint64, error) {
	for {
		page, err := s.Events(runID, after)
		if err != nil {
			return after, err
		}
		for _, ev := range page.Events {
			if ev.Seq <= after {
				continue
			}
			if err := emit(ev); err != nil {
				return after, err
			}
			after = ev.Seq
		}
		if !page.HasMore || len(page.Events) == 0 {
			return after, nil
		}
	}
}

func (s *WorkspaceService) read(root string) (workspace.Recovery, error) {
	if s.reader.Read == nil {
		return workspace.Recovery{}, errors.New("workspace reader unavailable")
	}
	return s.reader.Read(root)
}

// Recent lists remembered workspaces with status resolved from disk now.
// Nothing outside the remembered paths is scanned.
func (s *WorkspaceService) Recent() []RecentRun {
	entries := s.recent.List()
	out := make([]RecentRun, 0, len(entries))
	for _, e := range entries {
		out = append(out, s.resolve(e.Path))
	}
	return out
}

func (s *WorkspaceService) resolve(root string) RecentRun {
	r := RecentRun{Path: root}
	fi, err := os.Stat(root)
	if err != nil || !fi.IsDir() {
		r.Status = "unavailable"
		return r
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(runmodel.ManifestPath))); err != nil {
		if _, jerr := os.Stat(filepath.Join(root, filepath.FromSlash(runmodel.JournalPath))); jerr != nil {
			r.Status = "legacy" // v0.7 output or foreign folder: no workspace manifest/journal
			return r
		}
	}
	rec, err := s.read(root)
	ws := rec.Workspace
	r.Input, r.RunID = ws.Spec.Path, ws.Run.ID
	if !ws.Run.StartedAt.IsZero() {
		t := ws.Run.StartedAt
		r.StartedAt = &t
	}
	r.Targets = ws.Run.Targets.Total
	r.Failed = ws.Run.Targets.Error + ws.Run.Targets.Cancelled
	if ws.Run.Outcome != nil {
		r.Outcome = *ws.Run.Outcome
	}
	switch {
	case err != nil:
		r.Status, r.Error = "corrupt", err.Error()
	case rec.Active:
		r.Status = "running"
	case rec.Interrupted:
		r.Status = "interrupted"
	default:
		r.Status = "finished"
	}
	return r
}

// ImportLegacy imports old GUI localStorage output paths once. Legacy
// outcomes are never imported; status is always resolved from disk.
func (s *WorkspaceService) ImportLegacy(paths []string) (int, error) {
	return s.recent.ImportLegacy(paths)
}
