package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/UberMorgott/morgue/internal/recipe"
	"github.com/UberMorgott/morgue/internal/runmodel"
	"github.com/UberMorgott/morgue/internal/workspace"
)

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
func hashInput(ctx context.Context, role, path string) runmodel.InputFile {
	input := runmodel.InputFile{Role: role, Path: path, HashStatus: "error"}
	info, err := os.Stat(path)
	if err != nil {
		input.Error = err.Error()
		return input
	}
	if info.IsDir() {
		input.HashStatus = "pending"
		input.Error = "directory contents and exact consumed references are not hashed"
		return input
	}
	input.SizeBytes = info.Size()
	f, err := os.Open(path)
	if err != nil {
		input.Error = err.Error()
		return input
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.CopyBuffer(h, contextReader{ctx: ctx, reader: f}, make([]byte, 128*1024)); err != nil {
		input.Error = err.Error()
		return input
	}
	after, err := f.Stat()
	if err != nil || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		input.Error = "input changed while hashing"
		return input
	}
	sum := hex.EncodeToString(h.Sum(nil))
	input.SHA256 = &sum
	input.HashStatus = "complete"
	return input
}

type inputObserver struct {
	run *Run
	ctx context.Context
	id  string
}

func (o *inputObserver) Record(role, path string) error {
	input := hashInput(o.ctx, role, path)
	o.run.mu.Lock()
	defer o.run.mu.Unlock()
	target := o.run.targets[o.id]
	if target == nil {
		return errors.New("unknown input target")
	}
	for i, existing := range target.inputs {
		if existing.Role == role && pathKey(existing.Path) == pathKey(path) {
			target.inputs[i] = input
			return inputError(input)
		}
	}
	target.inputs = append(target.inputs, input)
	return inputError(input)
}
func inputError(input runmodel.InputFile) error {
	if input.HashStatus == "error" {
		return fmt.Errorf("hash input %s: %s", input.Path, input.Error)
	}
	return nil
}
func (r *Run) inputObserver(ctx context.Context, opts Options, path string) recipe.InputObserver {
	if r == nil {
		return nil
	}
	return &inputObserver{run: r, ctx: ctx, id: r.targetID(opts, path)}
}

func (r *Run) verifyTargetInputs(ctx context.Context, opts Options, tr reconInput) error {
	if r == nil {
		return nil
	}
	observer := r.inputObserver(ctx, opts, tr.path)
	if err := observer.Record("primary", tr.path); err != nil {
		return err
	}
	r.mu.Lock()
	id := r.keys[targetKey(opts, tr.path)]
	var current *string
	for _, input := range r.targets[id].inputs {
		if input.Role == "primary" {
			current = input.SHA256
		}
	}
	r.mu.Unlock()
	if tr.sha != "" && (current == nil || *current != tr.sha) {
		return errors.New("input changed after reconnaissance; start a fresh run")
	}
	for _, ref := range r.spec.ReferencePaths {
		if err := observer.Record("reference-search-directory", ref); err != nil {
			return err
		}
	}
	return nil
}

type reconInput struct{ path, sha string }

// productPath resolves existing products physically, rejecting lexical/reparse
// escapes unless the product lies within the explicitly selected external data.
func productPath(root, externalRoot, path string) (string, bool, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false, err
	}
	physical, err := workspace.PhysicalPath(abs)
	if err != nil {
		return "", false, err
	}
	realRoot, err := workspace.PhysicalPath(root)
	if err != nil {
		return "", false, err
	}
	rel, err := filepath.Rel(realRoot, physical)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
		return filepath.ToSlash(rel), false, nil
	}
	if externalRoot != "" {
		external, err := workspace.PhysicalPath(externalRoot)
		if err == nil {
			rel, err := filepath.Rel(external, physical)
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
				// The manifest's explicit-root policy uses the requested alias.
				// Physical paths prove containment but must not replace that link.
				return filepath.ToSlash(abs), true, nil
			}
		}
	}
	return "", false, errors.New("artifact escapes workspace and explicit game-data destination")
}

type artifactObserver struct {
	run      *Run
	ctx      context.Context
	id       string
	recipe   string
	complete bool
}

func (o *artifactObserver) Record(path, kind, stepID, tool string, shared bool) error {
	rel, external, err := productPath(o.run.spec.Output, o.run.spec.GameDataOut, path)
	if err != nil {
		return err
	}
	idTarget := o.id
	var targetID *string
	if !shared {
		targetID = &idTarget
	}
	identity := idTarget + "\x00" + rel + "\x00" + kind
	if shared {
		identity = "shared\x00" + rel + "\x00" + kind
	}
	sum := sha256.Sum256([]byte(identity))
	id := "a" + hex.EncodeToString(sum[:8])
	artifact := runmodel.Artifact{ID: id, TargetID: targetID, Path: rel, External: external, Kind: kind, Producer: runmodel.Producer{Recipe: o.recipe, StepID: stepID, Tool: tool}, HashStatus: "not-collected", Complete: o.complete, Warning: "exact tool identity/provenance is not collected in M2"}
	if !o.complete {
		artifact.Warning += "; target failed and this product may be partial"
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		input := hashInput(o.ctx, "artifact", path)
		artifact.SHA256 = input.SHA256
		artifact.HashStatus = input.HashStatus
		if input.HashStatus == "error" {
			return inputError(input)
		}
	}
	o.run.mu.Lock()
	defer o.run.mu.Unlock()
	if old, ok := o.run.artifacts[id]; ok && reflect.DeepEqual(old, artifact) {
		return nil
	}
	o.run.artifacts[id] = artifact
	parent := ""
	if target := o.run.targets[o.id]; target != nil {
		parent = target.model.ParentTargetID
	}
	eventTarget := o.id
	if shared {
		eventTarget = ""
		parent = ""
	}
	o.run.emitLocked(eventTarget, parent, time.Now().UTC(), runmodel.ArtifactWritten{Artifact: artifact})
	return o.run.persistenceErr
}
func (r *Run) artifactObserver(ctx context.Context, opts Options, path, recipeName string) recipe.ArtifactObserver {
	if r == nil {
		return nil
	}
	return &artifactObserver{run: r, ctx: ctx, id: r.targetID(opts, path), recipe: recipeName, complete: true}
}

func (r *Run) collectProducts(opts Options, tr TargetResult) error {
	if tr.Output == "" {
		return nil
	}
	ctx := context.Background()
	name := ""
	if tr.Recipe != nil {
		name = tr.Recipe.Name()
	}
	observer := r.artifactObserver(ctx, opts, tr.Recon.Path, name)
	observer.(*artifactObserver).complete = tr.Error == nil
	if original := filepath.Join(tr.Output, "original", filepath.Base(tr.Recon.Path)); tr.Recon.SHA256 != "" {
		if info, err := os.Stat(original); err == nil && !info.IsDir() {
			copied := hashInput(ctx, "original", original)
			if copied.SHA256 == nil || *copied.SHA256 != tr.Recon.SHA256 {
				return errors.New("captured original does not match reconnaissance input hash")
			}
		}
	}
	sourceKind := "decompiled-source"
	switch name {
	case "native", "ue5", "delphi":
		sourceKind = "pseudocode"
	case "unity-il2cpp":
		sourceKind = "stub"
	}
	for _, product := range []struct{ path, kind string }{{"original", "original"}, {"src", sourceKind}, {"dump", "stub"}, {"indexes", "index"}, {"recon.json", "metadata"}, {"index.json", "index"}} {
		path := filepath.Join(tr.Output, product.path)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		if err := observer.Record(path, product.kind, "output", "", false); err != nil {
			return err
		}
	}
	counts, err := outputCounts(tr.Output)
	r.mu.Lock()
	if target := r.targets[r.keys[targetKey(opts, tr.Recon.Path)]]; target != nil {
		target.files = counts
	}
	r.mu.Unlock()
	return err
}
func outputCounts(root string) (runmodel.FileCounts, error) {
	var counts runmodel.FileCounts
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("output contains a reparse or symlink product")
		}
		if d.IsDir() {
			if _, _, err := productPath(root, "", path); err != nil {
				return err
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		counts.Count++
		counts.Bytes += info.Size()
		return nil
	})
	return counts, err
}

func (r *Run) writeSummaryLocked() error {
	if r.spec.Output == "" {
		return nil
	}
	results := make([]TargetResult, 0, len(r.order))
	for _, id := range r.order {
		target := r.targets[id]
		tr := target.legacy
		if target.model.Error != "" && tr.Error == nil {
			tr.Error = errors.New(target.model.Error)
		}
		results = append(results, tr)
	}
	data, err := json.MarshalIndent(buildSummary(results, time.Since(r.handle.StartedAt)), "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(r.spec.Output, ".summary-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(r.spec.Output, "summary.json"))
}

func invokeRecipe(rec recipe.Recipe, ctx *recipe.Context) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("recipe %s panicked: %v", rec.Name(), value)
		}
	}()
	return rec.Execute(ctx)
}
