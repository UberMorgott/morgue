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
	"github.com/UberMorgott/morgue/internal/tools"
	"github.com/UberMorgott/morgue/internal/util"
	"github.com/UberMorgott/morgue/internal/workspace"
)

var (
	ErrInvalidRun   = errors.New("invalid run request")
	ErrOutputBusy   = errors.New("output is reserved by another run")
	preparedOutputs sync.Map
)

// Run owns a single top-level execution, including all recursively unpacked targets.
// The prepared run holds the workspace's OS lifetime lock until completion;
// process-local reservations also protect the user's requested output root.
type Run struct {
	mu             sync.Mutex
	engine         *Engine
	handle         runmodel.RunHandle
	spec           runmodel.RunSpec
	state          int // 0 prepared, 1 executing, 2 closed
	reservation    string
	events         chan<- runmodel.Event
	seq            uint64
	targets        map[string]*runTarget
	keys           map[string]string
	order          []string
	result         runmodel.RunResult
	eventErr       error
	store          workspace.Sink
	persistenceErr error
	cancel         context.CancelFunc
	products       *sharedProducts
	artifacts      map[string]runmodel.Artifact
	// overflow holds must-keep lifecycle/outcome events that could not be sent
	// because the consumer's buffer was momentarily full. They are re-delivered,
	// in order, on the next emit and in a bounded final flush, so a slow (but
	// draining) consumer never loses a terminal state. Guarded by mu.
	overflow []runmodel.Event
	// selection freezes every tool/runtime/helper identity used by this run;
	// engine.tools is its bound manager from before recon until completion.
	selection       *tools.RunSelection
	selectionErr    error
	toolsLockMu     sync.Mutex
	toolsLockSealed bool
}

// maxFinalFlush bounds the busy-wait that drains pending must-keep events at the
// end of a run. The budget resets on every successful delivery, so a draining
// consumer (however slow) always completes, while a consumer that reads nothing
// gives up quickly instead of deadlocking the engine (plan C2).
const maxFinalFlush = 50000

// mustKeep reports whether an event is a lifecycle/outcome record that must not
// be dropped under buffer pressure (as opposed to progress/log notifications).
func mustKeep(t runmodel.EventType) bool {
	switch t {
	case runmodel.EventRunStarted, runmodel.EventTargetDiscovered, runmodel.EventTargetFinished, runmodel.EventRunFinished:
		return true
	default:
		return false
	}
}

type runTarget struct {
	model     runmodel.TargetResult
	legacy    TargetResult
	started   time.Time
	finished  bool
	announced bool
	inputs    []runmodel.InputFile
	files     runmodel.FileCounts
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
	h, err := runmodel.NewHandle(output)
	if err != nil {
		return nil, err
	}
	if spec.GameDataOut != "" {
		spec.GameDataOut = filepath.Join(spec.GameDataOut, "run-"+h.ID)
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
	h.Output = output
	writer, err := workspace.Open(h, spec)
	if errors.Is(err, workspace.ErrNotEmpty) {
		name := strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))
		if name == "" || name == "." || name == string(filepath.Separator) {
			name = "run"
		}
		output = filepath.Join(output, name+"-"+h.ID)
		h.Output = output
		spec.Output = output
		writer, err = workspace.Open(h, spec)
	}
	if errors.Is(err, workspace.ErrLocked) {
		return nil, ErrOutputBusy
	}
	if err != nil {
		return nil, fmt.Errorf("prepare workspace: %w", err)
	}
	writer.SetPublisher(workspace.WriteNavigation)
	r := newRun(New(cfg, e.toolBaseDir), h, spec)
	r.store = writer
	r.reservation = key
	release = false
	return r, nil
}

func newRun(e *Engine, h runmodel.RunHandle, spec runmodel.RunSpec) *Run {
	return &Run{engine: e, handle: h, spec: spec, targets: map[string]*runTarget{}, keys: map[string]string{}, artifacts: map[string]runmodel.Artifact{}, products: newSharedProducts(context.Background())}
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
	return r.execute(ctx, pause, events, nil)
}

func (r *Run) execute(ctx context.Context, pause *PauseGate, events chan<- runmodel.Event, legacy chan<- PipelineEvent) (runmodel.RunResult, error) {
	r.mu.Lock()
	if r.state != 0 {
		r.mu.Unlock()
		return runmodel.RunResult{}, errors.New("run is already executed or closed")
	}
	r.state = 1
	runCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	r.products = newSharedProducts(runCtx)
	r.events = events
	// Bind the run-local tool manager before recon: every Resolve, install and
	// runtime lookup of this run (children included) goes through one selection.
	if sel, selErr := r.engine.tools.NewRunSelection(runCtx); selErr != nil {
		r.selectionErr = fmt.Errorf("select run tools: %w", selErr)
	} else {
		r.selection = sel
		r.engine.tools = sel.Bind()
	}
	r.emitLocked("", "", r.handle.StartedAt, runmodel.RunStarted{Handle: r.handle, Spec: r.Spec()})
	r.mu.Unlock()
	err := r.engine.runPipeline(runCtx, r.options(pause), legacy)
	if r.selection != nil {
		// Leases are released on every exit; the sealed lock stays on disk.
		if closeErr := r.selection.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("release run tools: %w", closeErr))
		}
	}
	r.mu.Lock()
	// Best-effort delivery of any must-keep events (e.g. run.finished) still
	// queued behind a momentarily full buffer, bounded so a dead consumer cannot
	// hang the engine.
	r.finalFlushLocked()
	result := r.result
	err = errors.Join(err, r.eventErr)
	if r.persistenceErr != nil {
		err = r.persistenceErr
		r.result.Outcome = OutcomeError
		r.result.Error = err.Error()
		result = r.result
	}
	if r.store != nil {
		closeErr := r.store.Close()
		r.store = nil
		if closeErr != nil {
			err = errors.Join(err, closeErr)
			result.Outcome = OutcomeError
			result.Error = err.Error()
		}
	}
	cancel()
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
	if r.store != nil {
		err := r.store.Close()
		r.store = nil
		if err != nil {
			if r.reservation != "" {
				preparedOutputs.Delete(r.reservation)
				r.reservation = ""
			}
			return err
		}
	}
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
	if opts.Depth == 0 {
		req := runmodel.RunRequest{Path: opts.Input, Output: opts.Output, Recipe: opts.Recipe, NoSkip: &opts.NoSkip, Exclude: opts.Exclude, ReferencePaths: opts.ReferencePaths, AllowDynamic: &opts.AllowDynamic, Cflow: &opts.Cflow, CodeOnly: &opts.CodeOnly, GameDataOut: opts.GameDataOut}
		r, err := e.PrepareRun(ctx, req)
		if err != nil {
			// Legacy renderers still receive a terminal diagnostic for a rejected
			// admission. There is no admitted workspace to journal in this case.
			em := emitter{ch: events}
			em.emitErr("scan", opts.Input, err)
			outcome := errorOutcome(err)
			em.send(PipelineEvent{Phase: outcome, Target: opts.Input, Done: true, RunOutcome: outcome, Error: err, OutputPath: opts.Output})
			return err
		}
		_, err = r.execute(ctx, opts.Pause, nil, events)
		return err
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
	if r.persistenceErr != nil {
		return
	}
	ev, err := runmodel.NewEvent(r.handle.ID, r.seq+1, ts, id, parent, p)
	if err != nil {
		r.eventErr = errors.Join(r.eventErr, err)
		return
	}
	if r.store != nil {
		if !r.appendLocked(&ev, p) {
			return
		}
		if ev.Type == runmodel.EventRunFinished {
			if err := r.store.Close(); err != nil {
				r.failPersistenceLocked(err)
				return
			}
			r.store = nil
		}
	}
	r.seq++
	if r.events == nil {
		return
	}
	// Deliver any backed-up must-keep events first, in order, so this event never
	// jumps ahead of an older one still waiting for buffer space.
	r.flushOverflowLocked()
	if len(r.overflow) > 0 {
		// Still backed up: preserve ordering. A must-keep event joins the queue;
		// a progress/log notification is dropped (RunResult stays authoritative).
		if mustKeep(ev.Type) {
			r.overflow = append(r.overflow, ev)
		}
		return
	}
	select {
	case r.events <- ev:
	default:
		// Buffer full: never stall the engine. A progress/log event is dropped;
		// a must-keep lifecycle/outcome event is staged for later delivery.
		if mustKeep(ev.Type) {
			r.overflow = append(r.overflow, ev)
		}
	}
}

// appendLocked durably records ev and reports whether it was committed. A
// navigation publisher failure (*workspace.PublishError) is an execution
// error but leaves the journal usable: a committed record is still delivered,
// and a refused successful run.finished is rewritten once as an error outcome
// (journal, manifest and RunResult agree; no durable success survives). Any
// other failure poisons persistence. Caller holds mu.
func (r *Run) appendLocked(ev *runmodel.Event, p runmodel.Payload) bool {
	err := r.store.Append(*ev)
	if err == nil {
		return true
	}
	var pe *workspace.PublishError
	if !errors.As(err, &pe) {
		r.failPersistenceLocked(err)
		return false
	}
	perr := fmt.Errorf("persist workspace: %w", err)
	r.eventErr = errors.Join(r.eventErr, perr)
	if pe.Committed {
		return true
	}
	rf, isTerminal := p.(runmodel.RunFinished)
	if !isTerminal {
		r.failPersistenceLocked(err)
		return false
	}
	rf.Outcome = OutcomeError
	rf.Error = errorText(errors.Join(nonEmptyErr(rf.Error), perr))
	retry, nerr := runmodel.NewEvent(ev.Run, ev.Seq, ev.TS, ev.TargetID, ev.ParentTargetID, rf)
	if nerr != nil {
		r.failPersistenceLocked(nerr)
		return false
	}
	r.result.Outcome, r.result.Error = OutcomeError, rf.Error
	if err := r.store.Append(retry); err != nil && !(errors.As(err, &pe) && pe.Committed) {
		r.failPersistenceLocked(err)
		return false
	}
	*ev = retry
	return true
}

func nonEmptyErr(s string) error {
	if s == "" {
		return nil
	}
	return errors.New(s)
}

func (r *Run) failPersistenceLocked(err error) {
	r.persistenceErr = errors.Join(r.persistenceErr, fmt.Errorf("persist workspace: %w", err))
	r.eventErr = errors.Join(r.eventErr, r.persistenceErr)
	if r.cancel != nil {
		r.cancel()
	}
}

// flushOverflowLocked re-delivers staged must-keep events in order, stopping at
// the first that still cannot be sent without blocking. Caller holds mu.
func (r *Run) flushOverflowLocked() {
	for len(r.overflow) > 0 {
		select {
		case r.events <- r.overflow[0]:
			r.overflow = r.overflow[1:]
		default:
			return
		}
	}
}

// finalFlushLocked makes a bounded best effort to deliver the remaining staged
// must-keep events (notably run.finished) once the run is over. The attempt
// budget resets on every delivery, so a draining consumer always drains while a
// dead consumer cannot hang the engine. Caller holds mu.
func (r *Run) finalFlushLocked() {
	if r.events == nil {
		return
	}
	for attempts := 0; len(r.overflow) > 0 && attempts < maxFinalFlush; attempts++ {
		select {
		case r.events <- r.overflow[0]:
			r.overflow = r.overflow[1:]
			attempts = 0
		default:
			runtime.Gosched()
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
	var sha *string
	if tr.Recon.SHA256 != "" {
		value := tr.Recon.SHA256
		sha = &value
	}
	if len(t.inputs) == 0 {
		status := "pending"
		if sha != nil {
			status = "complete"
		}
		t.inputs = []runmodel.InputFile{{Role: "primary", Path: m.InputPath, SHA256: sha, SizeBytes: tr.Recon.Size, HashStatus: status}}
	}
	startedAt := t.started.UTC()
	r.emitLocked(m.ID, m.ParentTargetID, time.Now().UTC(), runmodel.TargetDiscovered{SHA256: sha, InputSet: append([]runmodel.InputFile{}, t.inputs...), StartedAt: &startedAt, InputPath: m.InputPath, RelativeInput: m.RelativeInput, Kind: m.Kind, Recipe: m.Recipe, Output: m.Output, SizeBytes: tr.Recon.Size, Compiler: tr.Recon.Compiler, Obfuscator: tr.Recon.Obfuscator, Deobfuscator: resolveDeobfuscator(tr.Recon.Obfuscator), RecipeDesc: desc, ToolsNeeded: toolsNeeded})
}

func (r *Run) record(opts Options, tr TargetResult) TargetResult {
	r.collectQuality(opts, tr)
	if err := r.collectProducts(opts, tr); err != nil {
		tr.Error = errors.Join(tr.Error, err)
	}
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
	return tr
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
	prov := ""
	if t.model.Output != "" {
		prov = r.provenanceRel(t.model.Output)
	}
	r.emitLocked(t.model.ID, t.model.ParentTargetID, time.Now().UTC(), runmodel.TargetFinished{InputSet: append([]runmodel.InputFile{}, t.inputs...), Files: &t.files, Outcome: outcome, Recipe: t.model.Recipe, Output: t.model.Output, Error: errText, SkipReason: reason, DurationMs: t.model.DurationMs, Provenance: prov})
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
	input := hashInput(o.run.products.ctx, "primary", inputPath)
	reconResult := recon.Result{Path: inputPath, Kind: recon.Managed, Size: input.SizeBytes}
	if input.SHA256 != nil {
		reconResult.SHA256 = *input.SHA256
	}
	id := o.run.discover(opts, TargetResult{Recon: reconResult, Recipe: rec, Output: output})
	o.run.mu.Lock()
	o.run.targets[id].inputs = []runmodel.InputFile{input}
	o.run.mu.Unlock()
	return id, nil
}
func (o *childObserver) Finish(id, outcome, errText string) error {
	if outcome != OutcomeSuccess && outcome != OutcomeError && outcome != OutcomeCancelled && outcome != OutcomeSkipped {
		return fmt.Errorf("invalid child outcome %q", outcome)
	}
	r := o.run
	r.mu.Lock()
	t := r.targets[id]
	if t == nil || t.model.ParentTargetID != o.parent {
		r.mu.Unlock()
		return errors.New("unknown child target")
	}
	if t.finished {
		r.mu.Unlock()
		return errors.New("child target already finished")
	}
	tr := t.legacy
	r.mu.Unlock()
	if outcome == OutcomeError || outcome == OutcomeCancelled {
		tr.Error = errors.New(errText)
	}
	opts := o.opts
	opts.parentTargetID = o.parent
	var validationErr error
	if _, err := os.Stat(tr.Output); err == nil {
		qtr := tr
		qtr.Skipped = outcome == OutcomeSkipped
		if outcome == OutcomeCancelled {
			qtr.Error = errors.Join(qtr.Error, context.Canceled)
		}
		r.collectQuality(opts, qtr)
		validationErr = r.collectProducts(opts, tr)
	} else if outcome == OutcomeSuccess {
		validationErr = err
	}
	if validationErr != nil {
		outcome = OutcomeError
		errText = errors.Join(tr.Error, validationErr).Error()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	t = r.targets[id]
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
	return validationErr
}
