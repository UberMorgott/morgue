package engine

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/recipe"
	"github.com/UberMorgott/morgue/internal/recon"
	"github.com/UberMorgott/morgue/internal/runmodel"
	"github.com/UberMorgott/morgue/internal/scanner"
	"github.com/UberMorgott/morgue/internal/util"
)

var (
	ErrInvalidRun   = errors.New("invalid run request")
	ErrOutputBusy   = errors.New("output is reserved by another run")
	preparedOutputs sync.Map
)

// Run owns a single top-level execution, including all recursively unpacked targets.
// Output reservations are process-local in M1; durable OS locks arrive with M2.
type Run struct {
	mu          sync.Mutex
	engine      *Engine
	handle      runmodel.RunHandle
	spec        runmodel.RunSpec
	state       int // 0 prepared, 1 executing, 2 closed
	reservation string
	events      chan<- runmodel.Event
	seq         uint64
	targets     map[string]*runTarget
	keys        map[string]string
	order       []string
	result      runmodel.RunResult
	eventErr    error
}

type runTarget struct {
	model     runmodel.TargetResult
	legacy    TargetResult
	started   time.Time
	finished  bool
	announced bool
}

func cloneConfig(cfg config.Config) config.Config {
	if cfg.SkipCategories != nil {
		m := make(map[string]bool, len(cfg.SkipCategories))
		for k, v := range cfg.SkipCategories {
			m[k] = v
		}
		cfg.SkipCategories = m
	}
	cfg.CustomSkip = append([]string(nil), cfg.CustomSkip...)
	cfg.CustomInclude = append([]string(nil), cfg.CustomInclude...)
	return cfg
}

// PrepareRun validates user options before admission is reported to a client.
func (e *Engine) PrepareRun(ctx context.Context, req runmodel.RunRequest) (*Run, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	invalid := func(format string, args ...any) (*Run, error) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidRun, fmt.Sprintf(format, args...))
	}
	if strings.TrimSpace(req.Path) == "" {
		return invalid("path is required")
	}
	input, err := filepath.Abs(req.Path)
	if err != nil {
		return invalid("invalid input path")
	}
	if _, err := os.Stat(input); err != nil {
		return invalid("input is unavailable: %v", err)
	}
	if req.Recipe != "" && recipe.FindByName(req.Recipe) == nil {
		return invalid("unknown recipe %q", req.Recipe)
	}
	for _, pattern := range req.Exclude {
		if _, err := filepath.Match(pattern, ""); err != nil {
			return invalid("invalid exclude pattern %q", pattern)
		}
	}
	refs := make([]string, 0, len(req.ReferencePaths))
	seen := map[string]bool{}
	for _, ref := range req.ReferencePaths {
		abs, err := filepath.Abs(ref)
		if err != nil {
			return invalid("invalid reference path")
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			return invalid("reference directory is unavailable: %s", ref)
		}
		key := pathKey(abs)
		if !seen[key] {
			refs = append(refs, abs)
			seen[key] = true
		}
	}
	cfg := cloneConfig(e.cfg)
	if req.ToolsMirror != "" {
		cfg.ToolsMirror = req.ToolsMirror
	}
	if cfg.ToolsMirror != "" {
		u, err := url.Parse(cfg.ToolsMirror)
		if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return invalid("tools mirror must not contain credentials, query or fragment")
		}
	}
	output := req.Output
	if output == "" {
		output = cfg.DefaultOutputDir
	}
	if output == "" {
		output = util.AutoOutputRoot()
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return invalid("invalid output path")
	}
	gameData := req.GameDataOut
	if gameData != "" {
		gameData, err = filepath.Abs(gameData)
		if err != nil {
			return invalid("invalid game-data output path")
		}
	}
	value := func(v *bool, fallback bool) bool {
		if v != nil {
			return *v
		}
		return fallback
	}
	codeOnly := value(req.CodeOnly, false)
	spec := runmodel.RunSpec{Version: runmodel.SpecVersion, Path: input, Output: output, Recipe: req.Recipe, NoSkip: value(req.NoSkip, false), Exclude: append([]string{}, req.Exclude...), ReferencePaths: refs, AllowDynamic: value(req.AllowDynamic, cfg.AllowDynamicExecution), Cflow: value(req.Cflow, false), CodeOnly: codeOnly, ExtractAssets: !codeOnly && cfg.UnityExtractAssets, GameDataOut: gameData, ToolsMirror: cfg.ToolsMirror}
	if gameData != "" && spec.ExtractAssets {
		scan, err := e.Scan(input)
		if err != nil {
			return nil, fmt.Errorf("prepare game-data destination: %w", err)
		}
		roots := map[string]bool{}
		for _, group := range scan.Groups {
			if group.Kind == scanner.GroupUnityMono || group.Kind == scanner.GroupUnityIL2CPP {
				roots[pathKey(group.Root)] = true
			}
		}
		if len(roots) > 1 {
			return invalid("gameDataOut is ambiguous for multiple Unity games; run each game separately")
		}
	}
	key := pathKey(output)
	if _, loaded := preparedOutputs.LoadOrStore(key, struct{}{}); loaded {
		return nil, ErrOutputBusy
	}
	release := true
	defer func() {
		if release {
			preparedOutputs.Delete(key)
		}
	}()
	if err := os.MkdirAll(output, 0755); err != nil {
		return nil, fmt.Errorf("prepare output: %w", err)
	}
	// Detect unwritable output now, rather than returning an admitted handle first.
	f, err := os.CreateTemp(output, ".morgue-admission-*")
	if err != nil {
		return nil, fmt.Errorf("prepare output: %w", err)
	}
	tmp := f.Name()
	closeErr := f.Close()
	removeErr := os.Remove(tmp)
	if err := errors.Join(closeErr, removeErr); err != nil {
		return nil, fmt.Errorf("prepare output: %w", err)
	}
	h, err := runmodel.NewHandle(output)
	if err != nil {
		return nil, err
	}
	r := newRun(New(cfg, e.toolBaseDir), h, spec)
	r.reservation = key
	release = false
	return r, nil
}

func newRun(e *Engine, h runmodel.RunHandle, spec runmodel.RunSpec) *Run {
	return &Run{engine: e, handle: h, spec: spec, targets: map[string]*runTarget{}, keys: map[string]string{}}
}

func (r *Run) Handle() runmodel.RunHandle { return r.handle }
func (r *Run) Spec() runmodel.RunSpec {
	s := r.spec
	s.Exclude = append([]string{}, s.Exclude...)
	s.ReferencePaths = append([]string{}, s.ReferencePaths...)
	return s
}
func (r *Run) options(pause *PauseGate) Options {
	s := r.Spec()
	return Options{Input: s.Path, Output: s.Output, Recipe: s.Recipe, NoSkip: s.NoSkip, Exclude: s.Exclude, ReferencePaths: s.ReferencePaths, AllowDynamic: s.AllowDynamic, Cflow: s.Cflow, CodeOnly: s.CodeOnly, GameDataOut: s.GameDataOut, Pause: pause, run: r}
}

// Execute is single-use. Delivery is best-effort; RunResult is authoritative.
func (r *Run) Execute(ctx context.Context, pause *PauseGate, events chan<- runmodel.Event) (runmodel.RunResult, error) {
	r.mu.Lock()
	if r.state != 0 {
		r.mu.Unlock()
		return runmodel.RunResult{}, errors.New("run is already executed or closed")
	}
	r.state = 1
	r.events = events
	r.emitLocked("", "", r.handle.StartedAt, runmodel.RunStarted{Handle: r.handle, Spec: r.Spec()})
	r.mu.Unlock()
	err := r.engine.runPipeline(ctx, r.options(pause), nil)
	r.mu.Lock()
	result := r.result
	err = errors.Join(err, r.eventErr)
	r.state = 2
	if r.reservation != "" {
		preparedOutputs.Delete(r.reservation)
		r.reservation = ""
	}
	r.mu.Unlock()
	return result, err
}

func (r *Run) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state == 1 {
		return errors.New("cannot close an executing run")
	}
	r.state = 2
	if r.reservation != "" {
		preparedOutputs.Delete(r.reservation)
		r.reservation = ""
	}
	return nil
}

// Run preserves the old renderer contract while sharing the same target model.
func (e *Engine) Run(ctx context.Context, opts Options, events chan<- PipelineEvent) error {
	if opts.run != nil {
		return e.runPipeline(ctx, opts, events)
	}
	h, err := runmodel.NewHandle(opts.Output)
	if err != nil {
		return err
	}
	spec := runmodel.RunSpec{Version: 1, Path: opts.Input, Output: opts.Output, Recipe: opts.Recipe, NoSkip: opts.NoSkip, Exclude: append([]string{}, opts.Exclude...), ReferencePaths: append([]string{}, opts.ReferencePaths...), AllowDynamic: opts.AllowDynamic, Cflow: opts.Cflow, CodeOnly: opts.CodeOnly, ExtractAssets: !opts.CodeOnly && e.cfg.UnityExtractAssets, GameDataOut: opts.GameDataOut, ToolsMirror: e.cfg.ToolsMirror}
	opts.run = newRun(e, h, spec)
	return e.runPipeline(ctx, opts, events)
}

func pathKey(p string) string {
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}
func targetKey(opts Options, p string) string { return opts.parentTargetID + "\x00" + pathKey(p) }
func relativeInput(root, p string) string {
	if info, err := os.Stat(root); err == nil && !info.IsDir() {
		root = filepath.Dir(root)
	}
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == "." {
		rel = filepath.Base(p)
	}
	return filepath.ToSlash(rel)
}
func (r *Run) targetID(opts Options, p string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id := r.keys[targetKey(opts, p)]; id != "" {
		return id
	}
	return runmodel.TargetID(opts.parentTargetID, relativeInput(opts.Input, p))
}

func (r *Run) emitLocked(id, parent string, ts time.Time, p runmodel.Payload) {
	ev, err := runmodel.NewEvent(r.handle.ID, r.seq+1, ts, id, parent, p)
	if err != nil {
		r.eventErr = errors.Join(r.eventErr, err)
		return
	}
	r.seq++
	if r.events != nil {
		select {
		case r.events <- ev:
		default:
		}
	}
}

func (r *Run) discover(opts Options, tr TargetResult) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.discoverLocked(opts, tr)
}
func (r *Run) discoverLocked(opts Options, tr TargetResult) string {
	id := r.registerLocked(opts, tr)
	r.announceLocked(r.targets[id])
	return id
}

// register retains scanned/partially analyzed targets without publishing an
// incomplete discovery. Cancellation publishes them before their terminal event.
func (r *Run) register(opts Options, tr TargetResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.registerLocked(opts, tr)
}
func (r *Run) registerLocked(opts Options, tr TargetResult) string {
	key := targetKey(opts, tr.Recon.Path)
	if id := r.keys[key]; id != "" {
		t := r.targets[id]
		if tr.Recon.Kind != recon.Unknown {
			t.model.Kind = tr.Recon.Kind.String()
		}
		if tr.Recipe != nil {
			t.model.Recipe = tr.Recipe.Name()
		}
		if tr.Output != "" {
			t.model.Output = tr.Output
		}
		tr.ID = id
		tr.ParentTargetID = opts.parentTargetID
		t.legacy = tr
		return id
	}
	rel := relativeInput(opts.Input, tr.Recon.Path)
	base := runmodel.TargetID(opts.parentTargetID, rel)
	id := base
	for n := 2; r.targets[id] != nil; n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	name := ""
	if tr.Recipe != nil {
		name = tr.Recipe.Name()
	}
	tr.ID = id
	tr.ParentTargetID = opts.parentTargetID
	m := runmodel.TargetResult{ID: id, ParentTargetID: opts.parentTargetID, InputPath: tr.Recon.Path, RelativeInput: rel, Kind: tr.Recon.Kind.String(), Recipe: name, Output: tr.Output}
	r.targets[id] = &runTarget{model: m, legacy: tr, started: time.Now()}
	r.keys[key] = id
	r.order = append(r.order, id)
	return id
}

func (r *Run) announceLocked(t *runTarget) {
	if t.announced {
		return
	}
	t.announced = true
	m, tr := t.model, t.legacy
	desc := ""
	toolsNeeded := []string(nil)
	if tr.Recipe != nil {
		desc = tr.Recipe.Description()
		toolsNeeded = recipeTools(tr.Recipe, !r.spec.ExtractAssets)
		if display, ok := tr.Recipe.(interface{ DisplayTools() []string }); ok {
			toolsNeeded = display.DisplayTools()
		}
	}
	r.emitLocked(m.ID, m.ParentTargetID, time.Now().UTC(), runmodel.TargetDiscovered{InputPath: m.InputPath, RelativeInput: m.RelativeInput, Kind: m.Kind, Recipe: m.Recipe, Output: m.Output, SizeBytes: tr.Recon.Size, Compiler: tr.Recon.Compiler, Obfuscator: tr.Recon.Obfuscator, Deobfuscator: resolveDeobfuscator(tr.Recon.Obfuscator), RecipeDesc: desc, ToolsNeeded: toolsNeeded})
}

func (r *Run) record(opts Options, tr TargetResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := r.discoverLocked(opts, tr)
	t := r.targets[id]
	tr.ID = id
	tr.ParentTargetID = opts.parentTargetID
	t.legacy = tr
	outcome := errorOutcome(tr.Error)
	if tr.Skipped {
		outcome = OutcomeSkipped
	}
	r.finishTargetLocked(t, outcome, errorText(tr.Error), tr.SkipReason)
}
func errorText(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}
func (r *Run) finishTargetLocked(t *runTarget, outcome, errText, reason string) {
	if t.finished {
		return
	}
	r.announceLocked(t)
	t.finished = true
	t.model.Outcome = outcome
	t.model.Error = errText
	t.model.SkipReason = reason
	t.model.DurationMs = time.Since(t.started).Milliseconds()
	r.emitLocked(t.model.ID, t.model.ParentTargetID, time.Now().UTC(), runmodel.TargetFinished{Outcome: outcome, Recipe: t.model.Recipe, Output: t.model.Output, Error: errText, SkipReason: reason, DurationMs: t.model.DurationMs})
}

func (r *Run) legacyResults() []TargetResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	results := make([]TargetResult, 0, len(r.order))
	for _, id := range r.order {
		t := r.targets[id]
		if t.finished {
			results = append(results, t.legacy)
		}
	}
	return results
}
func countTargets(targets []runmodel.TargetResult, leaf bool) runmodel.Counts {
	parents := map[string]bool{}
	for _, t := range targets {
		parents[t.ParentTargetID] = true
	}
	var c runmodel.Counts
	for _, t := range targets {
		if leaf && parents[t.ID] {
			continue
		}
		c.Total++
		switch t.Outcome {
		case OutcomeSuccess:
			c.Success++
		case OutcomeError:
			c.Error++
		case OutcomeCancelled:
			c.Cancelled++
		case OutcomeSkipped:
			c.Skipped++
		default:
			c.Running++
		}
	}
	return c
}

func (r *Run) descendantError(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var failures []error
	for _, childID := range r.order {
		t := r.targets[childID]
		parent := t.model.ParentTargetID
		for parent != "" {
			if parent == id {
				if t.model.Outcome == OutcomeError || t.model.Outcome == OutcomeCancelled {
					cause := errors.New(t.model.Error)
					if t.model.Outcome == OutcomeCancelled {
						cause = fmt.Errorf("%w: %s", context.Canceled, t.model.Error)
					}
					failures = append(failures, cause)
				}
				break
			}
			p := r.targets[parent]
			if p == nil {
				break
			}
			parent = p.model.ParentTargetID
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("nested targets: %w", errors.Join(failures...))
	}
	return nil
}

func (r *Run) children(opts Options, parentPath string) recipe.ChildObserver {
	if r == nil {
		return nil
	}
	return &childObserver{run: r, opts: opts, parent: r.targetID(opts, parentPath)}
}

type childObserver struct {
	run    *Run
	opts   Options
	parent string
}

func (o *childObserver) Discover(inputPath, output, recipeName string) (string, error) {
	rec := recipe.FindByName(recipeName)
	if rec == nil {
		return "", fmt.Errorf("unknown child recipe %q", recipeName)
	}
	opts := o.opts
	opts.parentTargetID = o.parent
	return o.run.discover(opts, TargetResult{Recon: recon.Result{Path: inputPath, Kind: recon.Managed}, Recipe: rec, Output: output}), nil
}
func (o *childObserver) Finish(id, outcome, errText string) error {
	if outcome != OutcomeSuccess && outcome != OutcomeError && outcome != OutcomeCancelled && outcome != OutcomeSkipped {
		return fmt.Errorf("invalid child outcome %q", outcome)
	}
	r := o.run
	r.mu.Lock()
	defer r.mu.Unlock()
	t := r.targets[id]
	if t == nil || t.model.ParentTargetID != o.parent {
		return errors.New("unknown child target")
	}
	if t.finished {
		return errors.New("child target already finished")
	}
	if outcome == OutcomeError || outcome == OutcomeCancelled {
		if errText == "" {
			errText = "child execution failed"
		}
		t.legacy.Error = errors.New(errText)
	}
	t.legacy.Skipped = outcome == OutcomeSkipped
	r.finishTargetLocked(t, outcome, errText, "")
	return nil
}
