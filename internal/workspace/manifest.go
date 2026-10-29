package workspace

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"

	"github.com/UberMorgott/morgue/internal/runmodel"
)

const (
	stateRunning  = "running"
	stateFinished = "finished"
	statePending  = "pending"

	warnQuality = "quality not yet collected: every target quality is null until v0.8 M4; absence is not a pass"
)

var (
	appMu   sync.Mutex
	appInfo = defaultAppInfo()
)

// SetAppInfo records the application version/commit written into manifests.
// Developer builds keep "dev"/"unknown", which navigation labels as such.
func SetAppInfo(version, commit string) {
	appMu.Lock()
	defer appMu.Unlock()
	if version != "" {
		appInfo.Version = version
	}
	if commit != "" {
		appInfo.Commit = commit
	}
}

func currentAppInfo() runmodel.AppInfo {
	appMu.Lock()
	defer appMu.Unlock()
	return appInfo
}

func defaultAppInfo() runmodel.AppInfo {
	a := runmodel.AppInfo{Version: "dev", Commit: "unknown"}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" && s.Value != "" {
				a.Commit = s.Value
			}
		}
	}
	return a
}

// projector folds journal events into the manifest. Writer and recovery use
// the same projector, so a manifest at lastSeq N equals the replay of N.
type projector struct {
	ws       runmodel.Workspace
	idx      map[string]int
	paths    pathPolicy
	strict   bool // writer: path violations reject the event; recovery: diagnose
	started  bool
	finished bool
	diags    []string
}

func newProjector(strict bool) *projector {
	return &projector{idx: map[string]int{}, strict: strict}
}

func strPtr(s string) *string { return &s }

func timePtr(t time.Time) *time.Time { t = t.UTC(); return &t }

// clone returns an independent copy. Pointer fields are never mutated in
// place by apply (they are replaced), so a shallow element copy suffices.
func (p *projector) clone() runmodel.Workspace {
	ws := p.ws
	ws.Targets = append(make([]runmodel.TargetRecord, 0, len(p.ws.Targets)), p.ws.Targets...)
	ws.Artifacts = append(make([]runmodel.Artifact, 0, len(p.ws.Artifacts)), p.ws.Artifacts...)
	ws.Warnings = append(make([]string, 0, len(p.ws.Warnings)), p.ws.Warnings...)
	ws.Spec.Exclude = nonNil(ws.Spec.Exclude)
	ws.Spec.ReferencePaths = nonNil(ws.Spec.ReferencePaths)
	return ws
}

func (p *projector) restore(ws runmodel.Workspace, started, finished bool) {
	p.ws, p.started, p.finished = ws, started, finished
	p.idx = make(map[string]int, len(ws.Targets))
	for i, t := range ws.Targets {
		p.idx[t.ID] = i
	}
}

// checkpoint captures what apply(ev) may change, so a failed append can be
// undone. Publishing events take a full copy; progress/log events change only
// the run clock, lastSeq and at most one target, so they stay O(1).
func (p *projector) checkpoint(ev *runmodel.Event, full bool) func() {
	started, finished := p.started, p.finished
	if full || !started {
		ws := p.clone()
		return func() { p.restore(ws, started, finished) }
	}
	run, seq := p.ws.Run, p.ws.LastSeq
	i, ok := p.idx[ev.TargetID]
	var t runmodel.TargetRecord
	if ok {
		t = p.ws.Targets[i]
	}
	return func() {
		p.ws.Run, p.ws.LastSeq, p.started, p.finished = run, seq, started, finished
		if ok {
			p.ws.Targets[i] = t
		}
	}
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func (p *projector) bad(err error) error {
	if p.strict {
		return err
	}
	p.diags = append(p.diags, err.Error())
	return nil
}

// apply folds one validated event. Unknown event types are kept in the
// journal and skipped here.
func (p *projector) apply(ev *runmodel.Event) error {
	if ev.Type == runmodel.EventRunStarted {
		return p.start(ev)
	}
	if !p.started {
		return fmt.Errorf("event %d (%s) before run.started", ev.Seq, ev.Type)
	}
	if p.finished {
		return fmt.Errorf("event %d (%s) after run.finished", ev.Seq, ev.Type)
	}
	p.ws.LastSeq = ev.Seq
	if d := ev.TS.Sub(p.ws.Run.StartedAt).Milliseconds(); d > p.ws.Run.DurationMs {
		p.ws.Run.DurationMs = d
	}
	switch ev.Type {
	case runmodel.EventTargetDiscovered:
		var d runmodel.TargetDiscovered
		if err := decode(ev, &d); err != nil {
			return err
		}
		return p.discover(ev, d)
	case runmodel.EventStepProgress:
		if i, ok := p.idx[ev.TargetID]; ok && p.ws.Targets[i].State == statePending {
			t := &p.ws.Targets[i]
			t.State = stateRunning
			if t.StartedAt == nil {
				t.StartedAt = timePtr(ev.TS)
			}
			p.recount()
		}
	case runmodel.EventTargetFinished:
		var d runmodel.TargetFinished
		if err := decode(ev, &d); err != nil {
			return err
		}
		return p.finishTarget(ev, d)
	case runmodel.EventArtifactWritten:
		var d runmodel.ArtifactWritten
		if err := decode(ev, &d); err != nil {
			return err
		}
		return p.artifact(d.Artifact)
	case runmodel.EventQualityUpdated:
		var d runmodel.QualityUpdated
		if err := decode(ev, &d); err != nil {
			return err
		}
		if i, ok := p.idx[ev.TargetID]; ok {
			q := d.Quality
			p.ws.Targets[i].Quality = &q
		}
	case runmodel.EventRunFinished:
		var d runmodel.RunFinished
		if err := decode(ev, &d); err != nil {
			return err
		}
		p.finished = true
		r := &p.ws.Run
		r.State = stateFinished
		r.Outcome = strPtr(d.Outcome)
		r.FinishedAt = timePtr(d.FinishedAt)
		r.DurationMs = d.DurationMs
		r.Error = d.Error
		r.Targets = d.Targets
		r.LeafTargets = d.LeafTargets
	}
	return nil
}

func decode(ev *runmodel.Event, v any) error {
	if err := json.Unmarshal(ev.Data, v); err != nil {
		return fmt.Errorf("event %d (%s): bad payload: %w", ev.Seq, ev.Type, err)
	}
	return nil
}

func (p *projector) start(ev *runmodel.Event) error {
	if p.started {
		return fmt.Errorf("event %d: second run.started", ev.Seq)
	}
	var d runmodel.RunStarted
	if err := decode(ev, &d); err != nil {
		return err
	}
	if d.Handle.ID != ev.Run {
		return fmt.Errorf("run.started handle %q does not match envelope run %q", d.Handle.ID, ev.Run)
	}
	paths, err := newPathPolicy(d.Handle.Output, d.Spec.GameDataOut, p.strict)
	if err != nil {
		return err
	}
	p.paths = paths
	d.Spec.Exclude = nonNil(d.Spec.Exclude)
	d.Spec.ReferencePaths = nonNil(d.Spec.ReferencePaths)
	p.started = true
	p.ws = runmodel.Workspace{
		SchemaVersion:  runmodel.WorkspaceVersion,
		JournalVersion: runmodel.JournalVersion,
		EventVersion:   runmodel.EventVersion,
		App:            currentAppInfo(),
		Run:            runmodel.RunRecord{ID: ev.Run, StartedAt: d.Handle.StartedAt.UTC(), State: stateRunning},
		Spec:           d.Spec,
		LastSeq:        ev.Seq,
		Journal:        runmodel.JournalPath,
		ToolsLock:      runmodel.RunToolsLockPath,
		Targets:        []runmodel.TargetRecord{},
		Artifacts:      []runmodel.Artifact{},
		Warnings:       []string{warnQuality},
	}
	return nil
}

func (p *projector) target(ev *runmodel.Event) *runmodel.TargetRecord {
	if i, ok := p.idx[ev.TargetID]; ok {
		return &p.ws.Targets[i]
	}
	var parent *string
	if ev.ParentTargetID != "" {
		parent = strPtr(ev.ParentTargetID)
	}
	p.idx[ev.TargetID] = len(p.ws.Targets)
	p.ws.Targets = append(p.ws.Targets, runmodel.TargetRecord{
		ID: ev.TargetID, ParentTargetID: parent, InputSet: []runmodel.InputFile{}, State: statePending,
	})
	return &p.ws.Targets[len(p.ws.Targets)-1]
}

func (p *projector) discover(ev *runmodel.Event, d runmodel.TargetDiscovered) error {
	if ev.TargetID == "" {
		return fmt.Errorf("event %d: target.discovered without targetId", ev.Seq)
	}
	out, err := p.paths.relative(d.Output)
	if err != nil {
		if err = p.bad(fmt.Errorf("target %s output: %w", ev.TargetID, err)); err != nil {
			return err
		}
	}
	t := p.target(ev)
	t.InputPath, t.RelativeInput, t.Kind, t.Output = d.InputPath, d.RelativeInput, d.Kind, out
	if d.Recipe != "" {
		t.Recipe = d.Recipe
	}
	if d.SHA256 != nil {
		t.SHA256 = strPtr(*d.SHA256)
	}
	if d.InputSet != nil {
		t.InputSet = append([]runmodel.InputFile{}, d.InputSet...)
	}
	if d.StartedAt != nil {
		t.StartedAt = timePtr(*d.StartedAt)
	}
	p.recount()
	return nil
}

func (p *projector) finishTarget(ev *runmodel.Event, d runmodel.TargetFinished) error {
	if ev.TargetID == "" {
		return fmt.Errorf("event %d: target.finished without targetId", ev.Seq)
	}
	out, err := p.paths.relative(d.Output)
	if err != nil {
		if err = p.bad(fmt.Errorf("target %s output: %w", ev.TargetID, err)); err != nil {
			return err
		}
	}
	t := p.target(ev)
	t.State = stateFinished
	t.Outcome = strPtr(d.Outcome)
	t.Error, t.SkipReason, t.DurationMs = d.Error, d.SkipReason, d.DurationMs
	t.FinishedAt = timePtr(ev.TS)
	if d.Recipe != "" {
		t.Recipe = d.Recipe
	}
	if out != "" {
		t.Output = out
	}
	if d.InputSet != nil {
		t.InputSet = append([]runmodel.InputFile{}, d.InputSet...)
	}
	if d.Files != nil {
		t.Files = *d.Files
	}
	p.recount()
	return nil
}

func (p *projector) artifact(a runmodel.Artifact) error {
	if a.ID == "" {
		return p.bad(fmt.Errorf("artifact without id (%q)", a.Path))
	}
	path, external, err := p.paths.artifact(a.Path, a.External)
	if err != nil {
		return p.bad(fmt.Errorf("artifact %s: %w", a.ID, err))
	}
	a.Path, a.External = path, external
	if a.TargetID != nil {
		a.TargetID = strPtr(*a.TargetID)
	}
	if a.SHA256 != nil {
		a.SHA256 = strPtr(*a.SHA256)
	}
	for i := range p.ws.Artifacts {
		if p.ws.Artifacts[i].ID == a.ID {
			p.ws.Artifacts[i] = a // shared products are listed once
			return nil
		}
	}
	p.ws.Artifacts = append(p.ws.Artifacts, a)
	return nil
}

// recount derives live counters from target states; run.finished replaces
// them with the engine's authoritative totals.
func (p *projector) recount() {
	hasChild := make(map[string]bool, len(p.ws.Targets))
	for _, t := range p.ws.Targets {
		if t.ParentTargetID != nil {
			hasChild[*t.ParentTargetID] = true
		}
	}
	var all, leaf runmodel.Counts
	for _, t := range p.ws.Targets {
		addCount(&all, t)
		if !hasChild[t.ID] {
			addCount(&leaf, t)
		}
	}
	p.ws.Run.Targets, p.ws.Run.LeafTargets = all, leaf
}

func addCount(c *runmodel.Counts, t runmodel.TargetRecord) {
	c.Total++
	switch {
	case t.State == statePending:
		c.Pending++
	case t.State == stateRunning || t.Outcome == nil:
		c.Running++
	case *t.Outcome == runmodel.OutcomeSuccess:
		c.Success++
	case *t.Outcome == runmodel.OutcomeError:
		c.Error++
	case *t.Outcome == runmodel.OutcomeCancelled:
		c.Cancelled++
	default:
		c.Skipped++
	}
}

// replaceFile is the platform rename (MoveFileEx REPLACE_EXISTING on
// Windows); tests substitute a failing one.
var replaceFile = os.Rename

// writeAtomic writes data to a same-directory temp file, syncs it and renames
// it over path. On any failure the previous file stays intact and only the
// temp file this call created is removed.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	var rnd [6]byte
	_, _ = rand.Read(rnd[:])
	tmp := filepath.Join(dir, "."+filepath.Base(path)+"."+hex.EncodeToString(rnd[:])+".tmp")
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = replaceFile(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return syncDir(dir)
}

func encodeManifest(ws runmodel.Workspace) ([]byte, error) {
	b, err := json.MarshalIndent(ws, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
