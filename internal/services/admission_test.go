package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/engine"
	"github.com/UberMorgott/morgue/internal/runmodel"
)

type stubRun struct {
	h      runmodel.RunHandle
	exec   func(context.Context) error
	closed chan struct{}
	once   sync.Once
}

func (r *stubRun) Handle() runmodel.RunHandle { return r.h }
func (r *stubRun) Close() error {
	r.once.Do(func() { close(r.closed) })
	return nil
}
func (r *stubRun) Execute(ctx context.Context, _ *engine.PauseGate, events chan<- runmodel.Event) (runmodel.RunResult, error) {
	if ev, err := runmodel.NewEvent(r.h.ID, 1, r.h.StartedAt, "", "", runmodel.RunStarted{Handle: r.h, Spec: runmodel.RunSpec{Version: runmodel.SpecVersion}}); err == nil {
		events <- ev
	}
	err := r.exec(ctx)
	outcome := runmodel.OutcomeSuccess
	if err != nil {
		outcome = runmodel.OutcomeError
	}
	return runmodel.RunResult{Handle: r.h, Outcome: outcome}, err
}

func stubPrepare(exec func(context.Context) error, got chan<- runmodel.RunRequest) PrepareFunc {
	return func(_ context.Context, req runmodel.RunRequest) (AdmittedRun, error) {
		if got != nil {
			got <- req
		}
		h, err := runmodel.NewHandle(req.Output)
		if err != nil {
			return nil, err
		}
		return &stubRun{h: h, exec: exec, closed: make(chan struct{})}, nil
	}
}

func TestPipelineStartBusy(t *testing.T) {
	release := make(chan struct{})
	s := NewPipelineServiceWithPrepare(stubPrepare(func(context.Context) error {
		<-release
		return nil
	}, nil))
	done := make(chan error, 1)
	SetRunFinishedHook(s, func(err error) { done <- err })
	if err := s.Start("a", ""); err != nil {
		t.Fatalf("first start: %v", err)
	}
	if err := s.Start("b", ""); !errors.Is(err, ErrBusy) {
		t.Fatalf("second start: %v, want ErrBusy", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}
	st := s.GetStatus()
	if st.Running || st.Phase != "done" || st.Outcome != runmodel.OutcomeSuccess || st.RunID == "" || st.StartedAt == nil || st.LastSeq != 1 {
		t.Fatalf("status %+v", st)
	}
}

// StartSpec must hand the request to admission unchanged: dynamic/cflow and
// every other option survive the Wails/HTTP delegation path.
func TestStartSpecForwardsEveryField(t *testing.T) {
	yes, no := true, false
	want := runmodel.RunRequest{
		Path: "in.exe", Output: "out", Recipe: "dotnet", NoSkip: &yes,
		Exclude: []string{"*.pdb"}, ReferencePaths: []string{"refs"},
		AllowDynamic: &yes, Cflow: &yes, CodeOnly: &no,
		GameDataOut: "gd", ToolsMirror: "https://mirror.example",
	}
	got := make(chan runmodel.RunRequest, 1)
	s := NewPipelineServiceWithPrepare(stubPrepare(func(context.Context) error { return nil }, got))
	done := make(chan error, 1)
	SetRunFinishedHook(s, func(err error) { done <- err })
	h, err := s.StartSpec(want)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(<-got, want) {
		t.Fatal("request changed in delegation")
	}
	if h.ID == "" || h.StartedAt.IsZero() || h.Output != "out" {
		t.Fatalf("handle %+v", h)
	}
	if err := <-done; err != nil {
		t.Fatalf("run finished with error: %v", err)
	}
}

func TestStartSpecPrepareFailureReleasesSlot(t *testing.T) {
	bad := errors.New("bad request")
	fail := true
	var mu sync.Mutex
	ok := stubPrepare(func(context.Context) error { return nil }, nil)
	s := NewPipelineServiceWithPrepare(func(ctx context.Context, req runmodel.RunRequest) (AdmittedRun, error) {
		mu.Lock()
		defer mu.Unlock()
		if fail {
			return nil, bad
		}
		return ok(ctx, req)
	})
	if _, err := s.StartSpec(runmodel.RunRequest{Path: "x"}); !errors.Is(err, bad) {
		t.Fatalf("got %v, want prepare error", err)
	}
	if st := s.GetStatus(); st.Running || st.RunID != "" {
		t.Fatalf("slot still held: %+v", st)
	}
	mu.Lock()
	fail = false
	mu.Unlock()
	done := make(chan error, 1)
	SetRunFinishedHook(s, func(err error) { done <- err })
	if _, err := s.StartSpec(runmodel.RunRequest{Path: "x"}); err != nil {
		t.Fatalf("retry after failed prepare: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("run finished with error: %v", err)
	}
}

func TestEventHookAndStatusSeq(t *testing.T) {
	s := NewPipelineServiceWithPrepare(stubPrepare(func(context.Context) error { return nil }, nil))
	var seen []runmodel.Event
	var mu sync.Mutex
	var second []runmodel.Event
	AddEventHook(s, func(ev runmodel.Event) { mu.Lock(); seen = append(seen, ev); mu.Unlock() })
	AddEventHook(s, nil)
	AddEventHook(s, func(ev runmodel.Event) { mu.Lock(); second = append(second, ev); mu.Unlock() })
	h, err := s.RunSpec(runmodel.RunRequest{Path: "x"})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 || seen[0].Run != h.ID || seen[0].Type != runmodel.EventRunStarted {
		t.Fatalf("events %+v", seen)
	}
	if len(second) != 1 || second[0].Seq != seen[0].Seq {
		t.Fatalf("second hook did not receive the fan-out: %+v", second)
	}
	if st := s.GetStatus(); st.LastSeq != 1 || !st.StartedAt.Equal(h.StartedAt) {
		t.Fatalf("status %+v", st)
	}
}

func TestStopCancelsAndClosesRun(t *testing.T) {
	var run *stubRun
	s := NewPipelineServiceWithPrepare(func(ctx context.Context, req runmodel.RunRequest) (AdmittedRun, error) {
		r, err := stubPrepare(func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }, nil)(ctx, req)
		if err == nil {
			run, _ = r.(*stubRun)
		}
		return r, err
	})
	done := make(chan error, 1)
	SetRunFinishedHook(s, func(err error) { done <- err })
	if _, err := s.StartSpec(runmodel.RunRequest{Path: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := s.StopActive(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	select {
	case <-run.closed:
	case <-time.After(time.Second):
		t.Fatal("run not closed")
	}
	if st := s.GetStatus(); st.Phase != "cancelled" || st.Outcome != runmodel.OutcomeCancelled {
		t.Fatalf("status %+v", st)
	}
}

// With the real engine admission, an explicit false must override a true
// config default, and omission must inherit it.
func TestEngineAdmissionExplicitFalseOverridesConfig(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.exe")
	if err := os.WriteFile(in, []byte("MZ"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.AllowDynamicExecution = true
	no := false
	for _, tc := range []struct {
		name string
		v    *bool
		want bool
	}{{"omitted", nil, true}, {"explicit false", &no, false}} {
		t.Run(tc.name, func(t *testing.T) {
			run, err := engine.New(cfg, t.TempDir()).PrepareRun(t.Context(), runmodel.RunRequest{Path: in, Output: filepath.Join(t.TempDir(), "out"), AllowDynamic: tc.v})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = run.Close() }()
			if got := run.Spec().AllowDynamic; got != tc.want {
				t.Fatalf("allowDynamic=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestToolsStartInstallBusy(t *testing.T) {
	s := &ToolsService{installing: map[string]struct{}{}}
	// Simulate in-flight admissions without running real installs.
	s.installing["ilspycmd"] = struct{}{}
	if err := s.StartInstall("ilspycmd"); !errors.Is(err, ErrBusy) {
		t.Fatalf("same tool: %v, want ErrBusy", err)
	}
	if err := s.StartInstall(""); !errors.Is(err, ErrBusy) {
		t.Fatalf("install-all while one tool busy: %v, want ErrBusy", err)
	}
	delete(s.installing, "ilspycmd")
	s.installing[allToolsKey] = struct{}{}
	if err := s.StartInstall("ghidra"); !errors.Is(err, ErrBusy) {
		t.Fatalf("tool while install-all busy: %v, want ErrBusy", err)
	}
}
