package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/recipe"
	"github.com/UberMorgott/morgue/internal/recon"
	"github.com/UberMorgott/morgue/internal/runmodel"
	"github.com/UberMorgott/morgue/internal/tools"
)

const toolSelectionRecipeName = "__engine_tool_selection_test"

// toolSelectionProbe is set by the test that owns the fixture recipe.
var toolSelectionProbe struct {
	sync.Mutex
	toolBase    string
	invocations int
	errs        []error
}

type toolSelectionRecipe struct{}

func (toolSelectionRecipe) Name() string             { return toolSelectionRecipeName }
func (toolSelectionRecipe) Description() string      { return "M3 tool selection fixture" }
func (toolSelectionRecipe) Match(*recon.Result) bool { return false }
func (toolSelectionRecipe) Steps() []recipe.StepInfo { return nil }
func (toolSelectionRecipe) RequiredTools() []string  { return []string{"m3fake", "m3optional"} }
func (toolSelectionRecipe) Execute(ctx *recipe.Context) error {
	p := &toolSelectionProbe
	p.Lock()
	defer p.Unlock()
	p.invocations++
	fail := func(format string, args ...any) { p.errs = append(p.errs, fmt.Errorf(format, args...)) }
	// The exact identity is on disk before invocation.
	data, err := os.ReadFile(filepath.Join(ctx.SharedOut, filepath.FromSlash(runmodel.RunToolsLockPath)))
	if err != nil {
		fail("read run lock: %v", err)
	} else {
		var lock tools.RunToolsLock
		if err := json.Unmarshal(data, &lock); err != nil || lock.Complete || len(lock.Tools) == 0 || lock.Tools[0].Name != "m3fake" {
			fail("provisional run lock before invocation: %s (%v)", data, err)
		}
	}
	if _, err := ctx.Tools.Resolve("m3fake"); err != nil {
		fail("bound resolve: %v", err)
	}
	// Another manager (GUI/CLI/API) cannot swap or delete the leased payload.
	other := tools.NewManager(p.toolBase, config.Config{})
	if err := other.Delete("m3fake"); !errors.Is(err, tools.ErrInUse) {
		fail("delete during run: %v", err)
	}
	if _, err := other.InstallVersion(context.Background(), "m3fake", tools.InstallOptions{}, nil); !errors.Is(err, tools.ErrInUse) {
		fail("update during run: %v", err)
	}
	return os.WriteFile(filepath.Join(ctx.Output, "fixture.txt"), []byte("ok"), 0o644)
}

func init() { recipe.Register(toolSelectionRecipe{}) }

func TestRunSelectsEachToolOnceAndSealsLock(t *testing.T) {
	var heads, optionalProbes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/optional.exe" {
			optionalProbes.Add(1)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method == http.MethodHead {
			heads.Add(1)
		}
		w.Header().Set("Last-Modified", "Mon, 02 Jan 2023 15:04:05 GMT")
		_, _ = w.Write([]byte("payload"))
	}))
	defer srv.Close()
	orig := tools.Registry
	tools.Registry = []tools.ToolDef{
		{Name: "m3fake", Method: tools.MethodDirectURL, URL: srv.URL + "/tool.exe", Binary: "tool.exe"},
		{Name: "m3optional", Method: tools.MethodDirectURL, URL: srv.URL + "/optional.exe", Binary: "optional.exe", Optional: true},
	}
	t.Cleanup(func() { tools.Registry = orig })

	toolBase := t.TempDir()
	dir := filepath.Join(toolBase, "m3fake")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tool.exe"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".version"), []byte("2023.01.02"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &toolSelectionProbe
	p.Lock()
	p.toolBase, p.invocations, p.errs = toolBase, 0, nil
	p.Unlock()

	input := terminalInput(t, "a.dll", "b.dll", "c.dll")
	out := t.TempDir()
	e := New(config.Default(), toolBase)
	ch := make(chan PipelineEvent, 256)
	var warned atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range ch {
			if ev.Phase == "tools" && ev.Severity == "warn" {
				warned.Store(true)
			}
		}
	}()
	err := e.Run(context.Background(), Options{Input: input, Output: out, Recipe: toolSelectionRecipeName}, ch)
	close(ch)
	<-done
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	p.Lock()
	defer p.Unlock()
	if p.invocations != 3 {
		t.Fatalf("invocations = %d, want 3", p.invocations)
	}
	for _, err := range p.errs {
		t.Error(err)
	}
	if heads.Load() != 1 {
		t.Errorf("freshness probes of m3fake = %d, want exactly 1 per run", heads.Load())
	}
	if optionalProbes.Load() != 1 {
		t.Errorf("probes of missing optional tool = %d, want 1 per run", optionalProbes.Load())
	}
	if !warned.Load() {
		t.Error("missing optional tool must be an explicit warning")
	}
	data, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(runmodel.RunToolsLockPath)))
	if err != nil {
		t.Fatal(err)
	}
	var lock tools.RunToolsLock
	if err := json.Unmarshal(data, &lock); err != nil {
		t.Fatal(err)
	}
	if lock.SchemaVersion != 1 || !lock.Complete || len(lock.Tools) != 1 {
		t.Fatalf("final run lock: %s", data)
	}
	id := lock.Tools[0]
	if id.Name != "m3fake" || id.Version != "2023.01.02" || len(id.PayloadSHA256) != 64 || !id.Managed {
		t.Fatalf("identity: %+v", id)
	}
	// Leases are released after completion: mutation works again.
	if err := tools.NewManager(toolBase, config.Config{}).Delete("m3fake"); err != nil {
		t.Fatalf("delete after run: %v", err)
	}
}

// A failed freshness check keeps the working install and is reported as a
// tool-scoped warn record before that tool's ready event.
func TestFreshnessWarningNamesToolBeforeReady(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	orig := tools.Registry
	tools.Registry = []tools.ToolDef{
		{Name: "m3fake", Method: tools.MethodDirectURL, URL: srv.URL + "/tool.exe", Binary: "tool.exe"},
		{Name: "m3optional", Method: tools.MethodDirectURL, URL: srv.URL + "/optional.exe", Binary: "optional.exe", Optional: true},
	}
	t.Cleanup(func() { tools.Registry = orig })

	toolBase := t.TempDir()
	dir := filepath.Join(toolBase, "m3fake")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tool.exe"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &toolSelectionProbe
	p.Lock()
	p.toolBase, p.invocations, p.errs = toolBase, 0, nil
	p.Unlock()

	e := New(config.Default(), toolBase)
	ch := make(chan PipelineEvent, 256)
	var order []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range ch {
			if ev.Tool != "m3fake" {
				continue
			}
			switch {
			case ev.Severity == "warn" && ev.ToolStage == "":
				order = append(order, "warn")
			case ev.ToolStage == "ready":
				order = append(order, "ready")
			}
		}
	}()
	err := e.Run(context.Background(), Options{Input: terminalInput(t, "a.dll"), Output: t.TempDir(), Recipe: toolSelectionRecipeName}, ch)
	close(ch)
	<-done
	if err != nil {
		t.Fatalf("run must continue with the installed tool: %v", err)
	}
	if len(order) != 2 || order[0] != "warn" || order[1] != "ready" {
		t.Fatalf("m3fake events = %v, want [warn ready]", order)
	}
}

// The recon detector (diec) is selected through the run selection when it is
// installed, so its exact identity lands in the run tool lock.
func TestRunRecordsReconDetectorInLock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Last-Modified", "Mon, 02 Jan 2023 15:04:05 GMT")
		_, _ = w.Write([]byte("diec"))
	}))
	defer srv.Close()
	orig := tools.Registry
	tools.Registry = []tools.ToolDef{{Name: "diec", Method: tools.MethodDirectURL, URL: srv.URL + "/diec.exe", Binary: "diec.exe", Category: tools.CategoryDetector}}
	t.Cleanup(func() { tools.Registry = orig })
	toolBase := t.TempDir()
	dir := filepath.Join(toolBase, "diec")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "diec.exe"), []byte("diec"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, ".version"), []byte("2023.01.02"), 0o644)
	out := t.TempDir()
	e := New(config.Default(), toolBase)
	r, err := e.PrepareRun(context.Background(), runmodel.RunRequest{Path: terminalInput(t, "a.dll", "b.dll"), Output: out, Recipe: terminalRecipeName})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(runmodel.RunToolsLockPath)))
	if err != nil {
		t.Fatal(err)
	}
	var lock tools.RunToolsLock
	if err := json.Unmarshal(data, &lock); err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, id := range lock.Tools {
		if id.Name == "diec" {
			found++
		}
	}
	if found != 1 || !lock.Complete {
		t.Fatalf("diec not recorded once in sealed run lock: %s", data)
	}
}
