package runmodel

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestWorkspaceManifestWireContract(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	w := Workspace{SchemaVersion: WorkspaceVersion, JournalVersion: JournalVersion, EventVersion: EventVersion, App: AppInfo{Version: "dev", Commit: "unknown"}, Run: RunRecord{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", StartedAt: now, State: "running"}, Spec: RunSpec{Version: 1, Path: "game", Output: "out", Exclude: []string{}, ReferencePaths: []string{}}, LastSeq: 1, Journal: JournalPath, ToolsLock: RunToolsLockPath, Targets: []TargetRecord{}, Artifacts: []Artifact{}, Warnings: []string{}}
	data, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/morgue-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var gotJSON, wantJSON any
	if json.Unmarshal(data, &gotJSON) != nil || json.Unmarshal(want, &wantJSON) != nil {
		t.Fatal("invalid manifest JSON")
	}
	got, _ := json.Marshal(gotJSON)
	expected, _ := json.Marshal(wantJSON)
	if string(got) != string(expected) {
		t.Fatalf("manifest contract mismatch\ngot %s\nwant %s", got, expected)
	}
}
