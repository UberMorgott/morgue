package runmodel

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestEventWireContract(t *testing.T) {
	h := RunHandle{ID: strings.Repeat("a", 32), StartedAt: time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC), Output: "out"}
	spec := RunSpec{Version: 1, Path: "game", Output: "out", Exclude: []string{}, ReferencePaths: []string{}}
	ev, err := NewEvent(h.ID, 1, h.StartedAt, "", "", RunStarted{Handle: h, Spec: spec})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/run-started.json")
	if err != nil {
		t.Fatal(err)
	}
	var gotJSON, wantJSON any
	if json.Unmarshal(data, &gotJSON) != nil || json.Unmarshal(want, &wantJSON) != nil {
		t.Fatal("invalid event JSON")
	}
	got, _ := json.Marshal(gotJSON)
	wantBytes, _ := json.Marshal(wantJSON)
	if string(got) != string(wantBytes) {
		t.Fatalf("event contract mismatch\ngot %s\nwant %s", got, wantBytes)
	}
}

func TestEventRejectsInvalidLifecycleData(t *testing.T) {
	for _, tc := range []struct {
		name, run, target string
		seq               uint64
		payload           Payload
	}{
		{"invalid run", "bad", "", 1, LogRecord{Level: "info"}},
		{"zero sequence", strings.Repeat("a", 32), "", 0, LogRecord{Level: "info"}},
		{"missing target", strings.Repeat("a", 32), "", 1, TargetFinished{Outcome: "success"}},
		{"invalid outcome", strings.Repeat("a", 32), "t1", 1, TargetFinished{Outcome: "done"}},
		{"negative progress", strings.Repeat("a", 32), "t1", 1, StepProgress{Status: "running", Count: -1}},
		{"nil payload", strings.Repeat("a", 32), "", 1, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewEvent(tc.run, tc.seq, time.Now().UTC(), tc.target, "", tc.payload); err == nil {
				t.Fatal("accepted invalid event")
			}
		})
	}
}

func TestIdentityIncludesParentAndRelativePath(t *testing.T) {
	h, err := NewHandle("out")
	if err != nil {
		t.Fatal(err)
	}
	if len(h.ID) != 32 || h.StartedAt.Location() != time.UTC {
		t.Fatalf("invalid handle: %+v", h)
	}
	a := TargetID("", "a/game.dll")
	if a == TargetID("", "b/game.dll") || a == TargetID("parent", "a/game.dll") {
		t.Fatal("target identity lost parent or relative path")
	}
	if a != TargetID("", `a\game.dll`) {
		t.Fatal("path separators must normalize")
	}
}
