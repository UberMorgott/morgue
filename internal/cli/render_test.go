package cli

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/UberMorgott/morgue/internal/runmodel"
)

func mustEvent(t *testing.T, run string, seq uint64, target, parent string, p runmodel.Payload) runmodel.Event {
	t.Helper()
	ts := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	if seq > math.MaxInt32 {
		t.Fatalf("seq %d out of fixture range", seq)
	} else {
		ts = ts.Add(time.Duration(seq) * time.Second)
	}
	if rs, ok := p.(runmodel.RunStarted); ok {
		ts = rs.Handle.StartedAt
	}
	ev, err := runmodel.NewEvent(run, seq, ts, target, parent, p)
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func sampleStream(t *testing.T) []runmodel.Event {
	run := strings.Repeat("ab", 16)
	h := runmodel.RunHandle{ID: run, StartedAt: time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC), Output: "out"}
	tid := runmodel.TargetID("", "Game.exe")
	child := runmodel.TargetID(tid, "inner/Mod.dll")
	return []runmodel.Event{
		mustEvent(t, run, 1, "", "", runmodel.RunStarted{Handle: h, Spec: runmodel.RunSpec{Version: runmodel.SpecVersion}}),
		mustEvent(t, run, 2, "", "", runmodel.ScanCompleted{FilesTotal: 2, GroupsTotal: 1}),
		mustEvent(t, run, 3, tid, "", runmodel.TargetDiscovered{InputPath: `C:\g\Game.exe`, RelativeInput: "Game.exe", Kind: "dotnet", Recipe: "dotnet"}),
		mustEvent(t, run, 4, tid, "", runmodel.StepProgress{StepID: "ilspy", Name: "Decompile", Status: "running", StepIndex: 0, StepTotal: 2, Count: 5, CountTotal: 10, Unit: "types"}),
		mustEvent(t, run, 5, "", "", runmodel.ToolProgress{Tool: "ilspycmd", Stage: "download", Bytes: 100, BytesTotal: 200}),
		mustEvent(t, run, 6, tid, "", runmodel.LogRecord{Level: "warn", Phase: "execute", Message: "Step 9/9 done"}),
		mustEvent(t, run, 7, child, tid, runmodel.TargetDiscovered{InputPath: `C:\g\inner\Mod.dll`, RelativeInput: "inner/Mod.dll", Kind: "dotnet"}),
		mustEvent(t, run, 8, child, tid, runmodel.TargetFinished{Outcome: "error", Error: "ilspy crashed"}),
		mustEvent(t, run, 9, tid, "", runmodel.TargetFinished{Outcome: "error", Error: "child failed"}),
		mustEvent(t, run, 10, "", "", runmodel.RunFinished{Outcome: "error", FinishedAt: h.StartedAt.Add(time.Minute), DurationMs: 60000, Error: "1 target failed"}),
	}
}

func TestLineRendererTypedFields(t *testing.T) {
	var buf bytes.Buffer
	r := newLineRenderer(&buf)
	for _, ev := range sampleStream(t) {
		if err := r.Render(ev); err != nil {
			t.Fatal(err)
		}
	}
	want := strings.Join([]string{
		"[run] abababababababababababababababab started -> out",
		"[scan] 2 files, 1 groups",
		"[target] Game.exe (dotnet) recipe=dotnet",
		"[Game.exe] Step 1/2: Decompile — running 5/10 types",
		"[execute] WARN: Step 9/9 done",
		"[target] inner/Mod.dll (dotnet)",
		"[inner/Mod.dll] error: ilspy crashed",
		"[Game.exe] error: child failed",
		"[done] Pipeline error: 1 target failed",
		"",
	}, "\n")
	if buf.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestLineRendererRejectsDuplicateSeq(t *testing.T) {
	var once, twice bytes.Buffer
	stream := sampleStream(t)
	r1 := newLineRenderer(&once)
	for _, ev := range stream {
		_ = r1.Render(ev)
	}
	r2 := newLineRenderer(&twice)
	for range 2 {
		for _, ev := range stream {
			_ = r2.Render(ev)
		}
	}
	if once.String() != twice.String() {
		t.Fatalf("replayed stream printed again:\n%s", twice.String())
	}
}

func TestLineRendererSkipsUnknownType(t *testing.T) {
	var buf bytes.Buffer
	ev := runmodel.Event{Version: 2, Seq: 1, Run: strings.Repeat("0", 32), Type: "future.event", Data: json.RawMessage(`{}`)}
	if err := newLineRenderer(&buf).Render(ev); err != nil || buf.Len() != 0 {
		t.Fatalf("err=%v out=%q", err, buf.String())
	}
}

func parseRunFlags(t *testing.T, args ...string) runmodel.RunRequest {
	t.Helper()
	cmd := &cobra.Command{Use: "run", RunE: func(*cobra.Command, []string) error { return nil }}
	AddRunFlags(cmd)
	cmd.SetArgs(args)
	var req runmodel.RunRequest
	cmd.RunE = func(c *cobra.Command, a []string) error {
		req = RequestFromFlags(c, strings.Join(a, " "))
		return nil
	}
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	return req
}

// The CLI must produce the same RunRequest an HTTP client sends for the same
// options (the API decodes into runmodel.RunRequest; services forwards as is).
func TestRequestFromFlagsMatchesHTTPBody(t *testing.T) {
	got := parseRunFlags(t, "in.exe", "-o", "out", "--recipe", "dotnet", "--no-skip",
		"--exclude", "*.pdb", "--ref", "refs", "--allow-dynamic", "--cflow", "--code-only=false",
		"--gamedata-out", "gd", "--tools-mirror", "https://mirror.example")
	body := `{"path":"in.exe","output":"out","recipe":"dotnet","noSkip":true,"exclude":["*.pdb"],
		"referencePaths":["refs"],"allowDynamic":true,"cflow":true,"codeOnly":false,
		"gameDataOut":"gd","toolsMirror":"https://mirror.example"}`
	var want runmodel.RunRequest
	if err := json.Unmarshal([]byte(body), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cli %+v\nhttp %+v", got, want)
	}
}

func TestRequestFromFlagsOmittedVsExplicitFalse(t *testing.T) {
	omitted := parseRunFlags(t, "in.exe")
	if omitted.AllowDynamic != nil || omitted.Cflow != nil || omitted.CodeOnly != nil || omitted.NoSkip != nil {
		t.Fatalf("omitted flags must stay nil: %+v", omitted)
	}
	explicit := parseRunFlags(t, "in.exe", "--allow-dynamic=false")
	if explicit.AllowDynamic == nil || *explicit.AllowDynamic {
		t.Fatalf("explicit false lost: %+v", explicit)
	}
}
