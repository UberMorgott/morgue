package quality

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func unitydataCollect(t *testing.T, ev UnityDataEvidence) (string, []string, map[string]string) {
	t.Helper()
	dir := t.TempDir()
	b, _ := json.Marshal(ev)
	il2cppWrite(t, filepath.Join(dir, "evidence", "unitydata.json"), string(b))
	r, err := UnityData{}.Collect(context.Background(), Input{Output: dir})
	if err != nil {
		t.Fatal(err)
	}
	il2cppValidate(t, r)
	if r.Functions.Found.Status != "not-applicable" || r.Types.Emitted.Status != "not-applicable" {
		t.Fatal("asset totals must never appear as code coverage")
	}
	checks := map[string]string{}
	for _, c := range r.Checks {
		checks[c.ID] = c.Status
	}
	return r.Status, r.Warnings, checks
}

func TestUnityDataStatesAreDistinguishable(t *testing.T) {
	n, a := int64(120), int64(40)
	base := UnityDataEvidence{SchemaVersion: 1, Tool: "assetripper", ToolVersion: "1.3.4", GlobalGameManagersSHA256: "ab", Mode: "unity-project", ExportDir: "../_assets/G_Data-1/raw-export"}
	cases := []struct {
		state, want, present string
		files                *int64
	}{
		{UnityAssetsDisabled, "not-applicable", "not-applicable", nil},
		{UnityAssetsToolMissing, "degraded", "warn", nil},
		{UnityAssetsNoDataDir, "degraded", "warn", nil},
		{UnityAssetsFailed, "degraded", "fail", nil},
		{UnityAssetsExported, "pass", "pass", &n},
		{UnityAssetsReused, "pass", "pass", &n},
		{UnityAssetsExported, "degraded", "fail", new(int64)},
	}
	for _, c := range cases {
		ev := base
		ev.State, ev.Files, ev.AssetFiles, ev.Shared = c.state, c.files, &a, true
		status, _, checks := unitydataCollect(t, ev)
		if status != c.want || checks["output.present"] != c.present {
			t.Fatalf("%s: status %s checks %v", c.state, status, checks)
		}
	}
}

func TestUnityDataMissingIdentityAndEvidence(t *testing.T) {
	n := int64(3)
	status, _, checks := unitydataCollect(t, UnityDataEvidence{State: UnityAssetsExported, Files: &n, Tool: "assetripper"})
	if status != "degraded" || checks["provenance.complete"] != "warn" {
		t.Fatalf("%s %v", status, checks)
	}
	r, _ := UnityData{}.Collect(context.Background(), Input{Output: t.TempDir()})
	if r.Status != "unknown" {
		t.Fatal(r.Status)
	}
}
