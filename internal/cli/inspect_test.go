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
