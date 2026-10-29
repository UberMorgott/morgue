package quality

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/UberMorgott/morgue/internal/runmodel"
)

func natWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// nativeFixture writes a Ghidra target output with found functions of which
// the first emitted decompiled (and split), the rest failed.
func nativeFixture(t *testing.T, found, emitted int, defaults int) string {
	t.Helper()
	out := t.TempDir()
	src := filepath.Join(out, "src")
	var st, fn strings.Builder
	st.WriteString(`{"kind":"program","ghidraVersion":"12.2","imageBase":"0x140000000","pdbLoaded":false,"functionCount":` + fmt.Sprint(found) + "}\n")
	for i := 0; i < found; i++ {
		addr := fmt.Sprintf("0x%x", 0x140001000+i*0x10)
		name, source := fmt.Sprintf("func_%d", i), "IMPORTED"
		if i < defaults {
			name, source = fmt.Sprintf("FUN_%x", 0x140001000+i*0x10), "DEFAULT"
		}
		status := "failed"
		if i < emitted {
			status = "decompiled"
			fmt.Fprintf(&fn, `{"name":%q,"address":%q,"is_named":%t,"file":"functions/001/001_00.c"}`+"\n", name, addr, i >= defaults)
		}
		fmt.Fprintf(&st, `{"address":%q,"name":%q,"source":%q,"thunk":false,"status":%q,"warnings":1,"undefinedReturn":%t,"message":""}`+"\n",
			addr, name, source, status, i == 0)
	}
	fmt.Fprintf(&st, `{"kind":"summary","found":%d,"decompiled":%d,"errors":%d}`+"\n", found, emitted, found-emitted)
	natWrite(t, filepath.Join(src, GhidraStatusFile), st.String())
	natWrite(t, filepath.Join(src, "functions.ndjson"), fn.String())
	natWrite(t, filepath.Join(src, "game.c"), "// Decompiled by Ghidra via Morgue\n// PSEUDOCODE: Ghidra decompiler output reconstructed from machine code, not original source.\n")
	if err := WriteNativeEvidence(out, NativeEvidence{Family: "native",
		Decompile: &StepEvidence{Status: StepSucceeded, Tool: "ghidra", Binary: "game.exe", Output: "src"}}); err != nil {
		t.Fatal(err)
	}
	return out
}

func natValidate(t *testing.T, r runmodel.QualityReport) {
	t.Helper()
	for name, c := range map[string]runmodel.Coverage{"functions": r.Functions, "types": r.Types, "strings": r.StringDecryption} {
		if err := c.Validate(); err != nil {
			t.Errorf("%s coverage invalid: %v", name, err)
		}
	}
	for name, m := range map[string]runmodel.Metric{"decompilerErrors": r.DecompilerErrors, "unknownResultTypes": r.UnknownResultTypes,
		"unresolvedReferences": r.UnresolvedReferences, "residualObfuscation": r.ResidualObfuscation} {
		if err := m.Validate(); err != nil {
			t.Errorf("%s invalid: %v", name, err)
		}
	}
	if err := r.Names.Validate(); err != nil {
		t.Errorf("names invalid: %v", err)
	}
	if r.SchemaVersion != 1 || r.Warnings == nil || r.Checks == nil {
		t.Errorf("report envelope incomplete: %+v", r)
	}
}

func natCheckStatus(r runmodel.QualityReport, id string) string {
	for _, c := range r.Checks {
		if c.ID == id {
			return c.Status
		}
	}
	return "<missing>"
}

func TestNativeCoverageTenFoundEightEmitted(t *testing.T) {
	out := nativeFixture(t, 10, 8, 3)
	r, err := ForFamily("native").Collect(context.Background(), Input{Output: out})
	if err != nil {
		t.Fatal(err)
	}
	natValidate(t, r)
	if r.Functions.Ratio == nil || *r.Functions.Ratio != 0.8 || !r.Functions.Comparable {
		t.Fatalf("ratio = %v comparable=%v, want 0.8", r.Functions.Ratio, r.Functions.Comparable)
	}
	if *r.Functions.Found.Value != 10 || *r.Functions.Emitted.Value != 8 {
		t.Fatalf("found/emitted = %d/%d", *r.Functions.Found.Value, *r.Functions.Emitted.Value)
	}
	if r.DecompilerErrors.Status != "measured" || *r.DecompilerErrors.Value != 2 {
		t.Fatalf("decompilerErrors = %+v", r.DecompilerErrors)
	}
	if *r.UnknownResultTypes.Value != 1 {
		t.Fatalf("unknownResultTypes = %d", *r.UnknownResultTypes.Value)
	}
	if r.Status != "degraded" || natCheckStatus(r, "functions.coverage") != "warn" || natCheckStatus(r, "decompiler.errors") != "warn" {
		t.Fatalf("status = %s checks=%+v", r.Status, r.Checks)
	}
	if natCheckStatus(r, "artifact.fidelity") != "pass" {
		t.Fatalf("pseudocode banner not recognised: %+v", r.Checks)
	}
	// No provenance file yet: mandatory evidence missing is unknown, not pass.
	if natCheckStatus(r, "provenance.complete") != "unknown" {
		t.Fatalf("provenance check = %s", natCheckStatus(r, "provenance.complete"))
	}
	var msg string
	for _, c := range r.Checks {
		if c.ID == "functions.coverage" {
			msg = c.Message
		}
	}
	if !strings.Contains(msg, "3 of 10 default FUN_") {
		t.Fatalf("default-name ratio missing from %q", msg)
	}
	n := r.Names
	if n.Total.Status != "measured" || *n.Total.Value != 10 || *n.AutoNamed.Value != 3 || n.AutoRatio == nil || *n.AutoRatio != 0.3 {
		t.Fatalf("names = %+v", n)
	}
	if n.Inferred.Status != "unknown" || n.Inferred.Value != nil {
		t.Fatalf("absent name_map must leave inferred unknown, got %+v", n.Inferred)
	}
}

func TestNativeCleanPassesWithProvenance(t *testing.T) {
	out := nativeFixture(t, 4, 4, 0)
	var prov strings.Builder
	for i := 0; i < 4; i++ {
		a := fmt.Sprintf("0x%x", 0x140001000+i*0x10)
		b, _ := json.Marshal(runmodel.ProvenanceRecord{SchemaVersion: 1, Address: &a, ArtifactKind: runmodel.FidelityPseudocode,
			Producer: runmodel.ProvenanceProducer{Tool: "ghidra", StepID: "native.decompile"}})
		prov.Write(b)
		prov.WriteByte('\n')
	}
	natWrite(t, filepath.Join(out, ProvenanceFile), prov.String())
	r, _ := ForFamily("native").Collect(context.Background(), Input{Output: out})
	natValidate(t, r)
	if r.Status != "pass" || r.Functions.Ratio == nil || *r.Functions.Ratio != 1 {
		t.Fatalf("clean target: status=%s ratio=%v checks=%+v", r.Status, r.Functions.Ratio, r.Checks)
	}
}

func TestNativeMissingStatusIsUnknownNotZero(t *testing.T) {
	out := t.TempDir()
	natWrite(t, filepath.Join(out, "imports.json"), "[]")
	_ = WriteNativeEvidence(out, NativeEvidence{Family: "native",
		Decompile: &StepEvidence{Status: StepUnavailable, Reason: "ghidra not installed"}})
	r, _ := ForFamily("native").Collect(context.Background(), Input{Output: out})
	natValidate(t, r)
	if r.Functions.Found.Status != "unknown" || r.Functions.Found.Value != nil || r.Functions.Ratio != nil {
		t.Fatalf("functions must be unknown: %+v", r.Functions)
	}
	if !strings.Contains(r.Functions.Found.Basis, "ghidra not installed") {
		t.Fatalf("basis lacks reason: %q", r.Functions.Found.Basis)
	}
	if r.Status != "degraded" || natCheckStatus(r, "output.present") != "warn" {
		t.Fatalf("metadata-only output: status=%s checks=%+v", r.Status, r.Checks)
	}
}

func TestUE5AssetsOnlyIsNotApplicable(t *testing.T) {
	out := t.TempDir()
	natWrite(t, filepath.Join(out, "extracted", "Game", "A.uasset"), "x")
	natWrite(t, filepath.Join(out, "assets_index.json"), `{"assets_parsed":1,"assets_failed":0}`)
	_ = WriteNativeEvidence(out, NativeEvidence{Family: "ue5",
		Decompile:  &StepEvidence{Status: StepSkipped, Reason: "no UE shipping executable under Binaries/Win64"},
		Extraction: &ExtractionEvidence{Status: StepSucceeded, Dirs: 1, FilesProduced: 1},
		Usmap:      &UsmapEvidence{}})
	r, _ := ForFamily("ue5").Collect(context.Background(), Input{Output: out})
	natValidate(t, r)
	if r.Functions.Found.Status != "not-applicable" || r.Functions.Ratio != nil {
		t.Fatalf("assets-only functions must be not-applicable: %+v", r.Functions)
	}
	if r.Types.Found.Status != "unknown" || !strings.Contains(r.Types.Found.Basis, "no .usmap") {
		t.Fatalf("missing usmap must keep layout unknown: %+v", r.Types)
	}
	if natCheckStatus(r, "output.present") != "pass" {
		t.Fatalf("checks=%+v", r.Checks)
	}
}

func TestUE5EncryptedExtractionWarns(t *testing.T) {
	out := t.TempDir()
	_ = WriteNativeEvidence(out, NativeEvidence{Family: "ue5",
		Decompile:  &StepEvidence{Status: StepSkipped, Reason: "no exe"},
		Extraction: &ExtractionEvidence{Status: StepFailed, Dirs: 1, FailedDirs: 1, LastError: "container is encrypted, missing AES key"}})
	r, _ := ForFamily("ue5").Collect(context.Background(), Input{Output: out})
	natValidate(t, r)
	if natCheckStatus(r, "output.present") != "fail" || !strings.Contains(strings.Join(r.Warnings, "\n"), "no AES key") {
		t.Fatalf("encrypted extraction not surfaced: %+v / %v", r.Checks, r.Warnings)
	}
}

func TestDelphiWithoutIDRIsUnknownTypes(t *testing.T) {
	out := nativeFixture(t, 5, 5, 0)
	_ = os.Remove(filepath.Join(out, "src", "functions.ndjson")) // delphi does not split
	_ = WriteNativeEvidence(out, NativeEvidence{Family: "delphi",
		Decompile: &StepEvidence{Status: StepSucceeded},
		IDR:       &StepEvidence{Status: StepUnavailable, Reason: "idr not installed"}})
	r, _ := ForFamily("delphi").Collect(context.Background(), Input{Output: out})
	natValidate(t, r)
	if r.Types.Found.Status != "unknown" || r.Types.Found.Value != nil {
		t.Fatalf("delphi types must be unknown: %+v", r.Types)
	}
	if r.Functions.Ratio == nil || *r.Functions.Emitted.Value != 5 {
		t.Fatalf("delphi functions from status file: %+v", r.Functions)
	}
	if natCheckStatus(r, "output.present") != "warn" || r.Status != "degraded" {
		t.Fatalf("IDR missing must be visible: status=%s checks=%+v", r.Status, r.Checks)
	}
}

// TestCollectRealOutput prints the report for a real target output dir:
// MORGUE_QUALITY_DIR=<target out> MORGUE_QUALITY_FAMILY=native go test -run TestCollectRealOutput -v ./internal/quality
func TestCollectRealOutput(t *testing.T) {
	dir, fam := os.Getenv("MORGUE_QUALITY_DIR"), os.Getenv("MORGUE_QUALITY_FAMILY")
	if dir == "" || fam == "" {
		t.Skip("MORGUE_QUALITY_DIR/MORGUE_QUALITY_FAMILY not set")
	}
	r, err := ForFamily(fam).Collect(context.Background(), Input{Output: dir})
	if err != nil {
		t.Fatal(err)
	}
	natValidate(t, r)
	b, _ := json.MarshalIndent(r, "", "  ")
	t.Logf("%s", b)
	if p := os.Getenv("MORGUE_QUALITY_WRITE"); p != "" {
		_ = os.WriteFile(p, b, 0644)
	}
}
