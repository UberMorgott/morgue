package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/UberMorgott/morgue/internal/runmodel"
	"github.com/UberMorgott/morgue/internal/workspace"
)

func inspectRecovery(n int) workspace.Recovery {
	var r workspace.Recovery
	r.Workspace.Run.ID = strings.Repeat("b", 32)
	r.LastSeq = 42
	r.Interrupted = true
	for i := 0; i < n; i++ {
		r.Workspace.Targets = append(r.Workspace.Targets, runmodel.TargetRecord{
			ID: fmt.Sprintf("t%016x", i), State: "running", RelativeInput: fmt.Sprintf("f%d\nINJECT", i), Output: fmt.Sprintf("f%d", i),
		})
	}
	return r
}

func TestInspectInterruptedTextPagedAndReadOnly(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "morgue.json")
	_ = os.WriteFile(marker, []byte("stale"), 0o644)
	var out bytes.Buffer
	err := Inspect(InspectOptions{Root: root, Out: &out, Limit: 2, Offset: 1,
		Read: func(string) (workspace.Recovery, error) { return inspectRecovery(5), nil }})
	if err != nil {
		t.Fatal(err)
	}
	s := out.String()
	for _, want := range []string{"status: interrupted", "seq 42", "targets 2-3 of 5", "t0000000000000001", "more: rerun with --offset 3", "f1/INDEX.md"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in\n%s", want, s)
		}
	}
	if strings.Contains(s, "\nINJECT") || strings.Contains(s, "t0000000000000000 ") {
		t.Errorf("unbounded/unsanitized output:\n%s", s)
	}
	if b, _ := os.ReadFile(marker); string(b) != "stale" {
		t.Fatal("inspect modified the workspace")
	}
}

func TestInspectJSONCorruptReturnsPrefixAndError(t *testing.T) {
	root := t.TempDir()
	var out bytes.Buffer
	err := Inspect(InspectOptions{Root: root, Out: &out, JSON: true,
		Read: func(string) (workspace.Recovery, error) { return inspectRecovery(1), errors.New("seq gap at 7") }})
	if !errors.Is(err, ErrWorkspaceCorrupt) {
		t.Fatalf("err = %v", err)
	}
	var rep InspectReport
	if jerr := json.Unmarshal(out.Bytes(), &rep); jerr != nil {
		t.Fatal(jerr)
	}
	if rep.Status != "corrupt" || rep.LastSeq != 42 || len(rep.TargetPage) != 1 || rep.Error != "seq gap at 7" {
		t.Fatalf("report %+v", rep)
	}
}

func TestInspectPerTargetQuality(t *testing.T) {
	rec := inspectRecovery(2)
	ratio, auto := 0.5, 0.25
	two, zero, eight := int64(2), int64(0), int64(8)
	m := func(v *int64) runmodel.Metric { return runmodel.Metric{Value: v, Status: "measured"} }
	rec.Workspace.Targets[0].Quality = &runmodel.QualityReport{
		Status: "degraded", Family: "native",
		Functions:        runmodel.Coverage{Found: m(&two), Emitted: m(&two), Ratio: &ratio},
		DecompilerErrors: m(&zero),
		Names:            runmodel.NameQuality{Total: m(&eight), AutoNamed: m(&two), AutoRatio: &auto},
	}
	id0 := rec.Workspace.Targets[0].ID
	rec.Workspace.Artifacts = []runmodel.Artifact{
		{TargetID: &id0, Kind: runmodel.FidelityPseudocode}, {TargetID: &id0, Kind: runmodel.FidelityPseudocode},
		{TargetID: &id0, Kind: "metadata"},
	}
	read := func(string) (workspace.Recovery, error) { return rec, nil }

	var out bytes.Buffer
	if err := Inspect(InspectOptions{Root: t.TempDir(), Out: &out, Read: read}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"quality: degraded (native) | functions 50.0% | decompiler errors 0 | auto names 25.0% | fidelity pseudocode x2",
		"quality: not-collected | fidelity none",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in\n%s", want, out.String())
		}
	}

	out.Reset()
	if err := Inspect(InspectOptions{Root: t.TempDir(), Out: &out, JSON: true, Read: read}); err != nil {
		t.Fatal(err)
	}
	var rep InspectReport
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.Quality) != 2 || rep.Quality[0].Status != "degraded" || rep.Quality[0].Fidelity[runmodel.FidelityPseudocode] != 2 ||
		rep.Quality[0].NamesAutoRatio == nil || *rep.Quality[0].NamesAutoRatio != 0.25 || rep.Quality[1].Status != "not-collected" {
		t.Fatalf("quality %+v", rep.Quality)
	}
}

func TestInspectRebuildUsesRebuilder(t *testing.T) {
	root := t.TempDir()
	called := ""
	cmd := NewInspectCmd(
		func(string) (workspace.Recovery, error) { called = "read"; return workspace.Recovery{}, nil },
		func(string) (workspace.Recovery, error) { called = "rebuild"; return workspace.Recovery{}, nil })
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{root, "--rebuild-manifest"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if called != "rebuild" || !strings.Contains(out.String(), "manifest: rebuilt") {
		t.Fatalf("called=%s out=%s", called, out.String())
	}
}

// Acceptance: a 100k-event interrupted journal inspects within 2 s via the
// real reader, and inspect does not modify the workspace.
func TestInspect100kInterruptedJournalUnder2s(t *testing.T) {
	if testing.Short() {
		t.Skip("large journal")
	}
	root := filepath.Join(t.TempDir(), "out")
	id := strings.Repeat("c", 32)
	t0 := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	h := runmodel.RunHandle{ID: id, StartedAt: t0, Output: root}
	spec := runmodel.RunSpec{Version: runmodel.SpecVersion, Path: "game", Output: root, Exclude: []string{}, ReferencePaths: []string{}}
	w, err := workspace.Open(h, spec)
	if err != nil {
		t.Fatal(err)
	}
	for seq := uint64(1); seq <= 100000; seq++ {
		var p runmodel.Payload = runmodel.LogRecord{Level: "info", Message: "line"}
		if seq == 1 {
			p = runmodel.RunStarted{Handle: h, Spec: spec}
		}
		ev, err := runmodel.NewEvent(id, seq, t0.Add(time.Duration(seq-1)*time.Microsecond), "", "", p)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.Append(ev); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil { // no run.finished: simulated crash
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(root, runmodel.ManifestPath))
	var out bytes.Buffer
	start := time.Now()
	if err := Inspect(InspectOptions{Root: root, Out: &out}); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("inspect took %v", d)
	}
	if !strings.Contains(out.String(), "status: interrupted") || !strings.Contains(out.String(), "seq 100000") {
		t.Fatalf("out:\n%s", out.String())
	}
	if after, _ := os.ReadFile(filepath.Join(root, runmodel.ManifestPath)); !bytes.Equal(before, after) {
		t.Fatal("inspect modified morgue.json")
	}
}
