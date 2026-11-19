package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/UberMorgott/morgue/internal/provenance"
	"github.com/UberMorgott/morgue/internal/quality"
	"github.com/UberMorgott/morgue/internal/runmodel"
)

// provenanceSink binds a target's provenance.ndjson to recipe.Context. It
// fills the engine-owned identity (targetId, inputSha256) that recipes leave
// empty, and keeps the stream additive across batch phases of one target.
type provenanceSink struct {
	w        *provenance.Writer
	path     string
	targetID string
	inputSHA string
	rows     atomic.Int64
}

func (s *provenanceSink) Write(r runmodel.ProvenanceRecord) error {
	if r.SchemaVersion == 0 {
		r.SchemaVersion = runmodel.ProvenanceVersion
	}
	if r.TargetID == "" {
		r.TargetID = s.targetID
	}
	if r.InputSHA256 == "" {
		r.InputSHA256 = s.inputSHA
	}
	if r.Evidence == nil {
		r.Evidence = []runmodel.Evidence{}
	}
	if r.Producer.Args == nil {
		r.Producer.Args = []string{}
	}
	if err := s.w.Write(r); err != nil {
		return err
	}
	s.rows.Add(1)
	return nil
}

// openProvenance opens <targetOutput>/provenance.ndjson for appending. A
// failure is returned so the recipe still runs (with a nil sink) and the
// caller can surface the missing provenance as a warning.
func (r *Run) openProvenance(opts Options, filePath, targetOutput, inputSHA string) (*provenanceSink, error) {
	p := filepath.Join(targetOutput, quality.ProvenanceFile)
	w, err := provenance.Open(p)
	if err != nil {
		return nil, err
	}
	s := &provenanceSink{w: w, path: p, inputSHA: inputSHA}
	if r != nil {
		s.targetID = r.targetID(opts, filePath)
	}
	return s, nil
}

// close syncs the stream and removes it when nothing was ever written to it
// (an absent file is honest "no provenance"; an empty one would look complete).
func (s *provenanceSink) close() error {
	if s == nil {
		return nil
	}
	err := s.w.Close()
	if info, serr := os.Stat(s.path); serr == nil && info.Size() == 0 {
		_ = os.Remove(s.path)
	}
	return err
}

// recipeSink returns the sink as recipe.Context.Provenance, or a nil
// interface when no stream is open (a typed nil would not compare nil).
func (s *provenanceSink) recipeSink() provenance.Sink {
	if s == nil {
		return nil
	}
	return s
}

// provenanceRel is the workspace-relative provenance stream of a target
// output, or "" when the target streamed none.
func (r *Run) provenanceRel(targetOutput string) string {
	p := filepath.Join(targetOutput, quality.ProvenanceFile)
	info, err := os.Stat(p)
	if err != nil || info.Size() == 0 || r.spec.Output == "" {
		return ""
	}
	rel, err := filepath.Rel(r.spec.Output, p)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || len(rel) > 2 && rel[:3] == ".."+string(filepath.Separator) {
		return ""
	}
	return filepath.ToSlash(rel)
}

// collectQuality measures a finished target with its recipe's family
// collectors, writes <target>/quality.json, records it and publishes
// quality.updated. Collection never fails the target: a collector problem is
// an explicit unknown report with a warning. Skipped and cancelled targets get
// no report (null = not collected).
func (r *Run) collectQuality(opts Options, tr TargetResult) {
	if r == nil || tr.Output == "" || tr.Skipped || errors.Is(tr.Error, context.Canceled) {
		return
	}
	if info, err := os.Stat(tr.Output); err != nil || !info.IsDir() {
		return
	}
	name := ""
	if tr.Recipe != nil {
		name = tr.Recipe.Name()
	}
	id := r.discover(opts, tr)
	r.mu.Lock()
	t := r.targets[id]
	rec := runmodel.TargetRecord{ID: id, InputPath: t.model.InputPath, RelativeInput: t.model.RelativeInput,
		Kind: t.model.Kind, Recipe: name, Output: tr.Output, InputSet: append([]runmodel.InputFile{}, t.inputs...)}
	if t.model.ParentTargetID != "" {
		parent := t.model.ParentTargetID
		rec.ParentTargetID = &parent
	}
	if tr.Recon.SHA256 != "" {
		sha := tr.Recon.SHA256
		rec.SHA256 = &sha
	}
	r.mu.Unlock()

	report := quality.CollectTarget(context.Background(), name, quality.Input{Target: rec, Output: tr.Output})
	path := filepath.Join(tr.Output, quality.QualityFile)
	data, err := json.MarshalIndent(report, "", "  ")
	if err == nil {
		err = os.WriteFile(path, append(data, '\n'), 0o644)
	}
	if err != nil {
		report.Warnings = append(report.Warnings, "quality.json not written: "+err.Error())
	} else {
		observer := r.newArtifactObserver(context.Background(), opts, tr.Recon.Path, name)
		observer.complete = tr.Error == nil
		if rerr := observer.Record(path, "metadata", "quality", "morgue", false); rerr != nil {
			report.Warnings = append(report.Warnings, "quality.json not recorded: "+rerr.Error())
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.emitLocked(id, t.model.ParentTargetID, time.Now().UTC(), runmodel.QualityUpdated{Quality: report})
}
