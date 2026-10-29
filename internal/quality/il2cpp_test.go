package quality

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/UberMorgott/morgue/internal/runmodel"
)

const il2cppStubCS = `using Il2CppInspector.DLL;
using UnityEngine;

namespace Game.Core;

[Token(Token = "0x02000002")]
public class Player : MonoBehaviour
{
	[Token(Token = "0x04000001")]
	[FieldOffset(Offset = "0x20")]
	private int hp;

	[Token(Token = "0x06000001")]
	[Address(RVA = "0x007F98F0", Offset = "0x7F84F0", VA = "0x00000001807F98F0")]
	public void Hit(int dmg)
	{
	}

	[Token(Token = "0x06000002")]
	public abstract int Score();

	[Token(Token = "0x02000003")]
	private class Inner
	{
		[Token(Token = "0x06000003")]
		[Address(RVA = "0x00001000", Offset = "0x400", VA = "0x0000000180001000")]
		public Inner()
		{
		}
	}
}
`

const il2cppInspectorHeader = `/*
 * Generated code file by Il2CppInspector
 */
// Image 0: Game.Core.dll - Assembly: Game.Core, Version=0.0.0.0 - Types 0-2
// Image 1: mscorlib.dll - Assembly: mscorlib, Version=4.0.0.0 - Types 3-9
namespace Game.Core
{
	public class Player // TypeDefIndex: 1
	{
		public void Hit(int dmg); // 0x00000001807F98F0-0x00000001807F9900
	}
}
`

func il2cppWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func il2cppSum(s string) *string {
	h := sha256.Sum256([]byte(s))
	v := hex.EncodeToString(h[:])
	return &v
}

// il2cppFixture builds a stub target; mutate lets a test corrupt one aspect.
func il2cppFixture(t *testing.T, header string) (string, Input) {
	t.Helper()
	dir := t.TempDir()
	il2cppWrite(t, filepath.Join(dir, "src", "Game.Core", "Game.Core", "Player.cs"), il2cppStubCS)
	il2cppWrite(t, filepath.Join(dir, "dump", "cs", "il2cpp.cs"), header)
	il2cppWrite(t, filepath.Join(dir, "dump", "dll", "Game.Core.dll"), "MZ")
	il2cppWrite(t, filepath.Join(dir, "dump", "dll", "mscorlib.dll"), "MZ")
	il2cppWrite(t, filepath.Join(dir, "dump", "dll", "Il2CppInspector.dll"), "MZ")
	il2cppWrite(t, filepath.Join(dir, "original", "GameAssembly.dll"), "binary")
	il2cppWrite(t, filepath.Join(dir, "original", "global-metadata.dat"), "metadata")
	v := int64(31)
	ev := IL2CPPEvidence{SchemaVersion: 1, Binary: "original/GameAssembly.dll", Metadata: "original/global-metadata.dat",
		MetadataVersion: &v, Dumper: "il2cppinspector", DumperVersion: "2026.1", DumpCS: "dump/cs/il2cpp.cs",
		Attempts:   []DumperAttempt{{Tool: "il2cppinspector", Version: "2026.1"}},
		Assemblies: []AssemblyResult{{Name: "Game.Core.dll", Status: "decompiled", Mode: "project"}, {Name: "mscorlib.dll", Status: "system-skipped"}}}
	b, _ := json.Marshal(ev)
	il2cppWrite(t, filepath.Join(dir, "evidence", "il2cpp.json"), string(b))
	in := Input{Output: dir, Target: runmodel.TargetRecord{InputSet: []runmodel.InputFile{
		{Role: "primary", SHA256: il2cppSum("binary"), HashStatus: "complete"},
		{Role: "il2cpp-metadata", SHA256: il2cppSum("metadata"), HashStatus: "complete"},
	}}}
	return dir, in
}

func il2cppValidate(t *testing.T, r runmodel.QualityReport) {
	t.Helper()
	for name, c := range map[string]runmodel.Coverage{"functions": r.Functions, "types": r.Types, "strings": r.StringDecryption} {
		if err := c.Validate(); err != nil {
			t.Fatalf("%s coverage invalid: %v", name, err)
		}
	}
	for name, m := range map[string]runmodel.Metric{"decompilerErrors": r.DecompilerErrors, "unknownResultTypes": r.UnknownResultTypes, "unresolvedReferences": r.UnresolvedReferences, "residualObfuscation": r.ResidualObfuscation} {
		if err := m.Validate(); err != nil {
			t.Fatalf("%s invalid: %v", name, err)
		}
	}
	if err := r.Names.Validate(); err != nil {
		t.Fatalf("names invalid: %v", err)
	}
	if r.SchemaVersion != 1 || r.Warnings == nil || r.Checks == nil {
		t.Fatalf("report shape: %+v", r)
	}
}

func il2cppCheck(t *testing.T, r runmodel.QualityReport, id string) runmodel.QualityCheck {
	t.Helper()
	for _, c := range r.Checks {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("check %s missing", id)
	return runmodel.QualityCheck{}
}

func TestIL2CPPStubNeverCountsRecoveredBodies(t *testing.T) {
	_, in := il2cppFixture(t, il2cppInspectorHeader)
	r, err := IL2CPP{}.Collect(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	il2cppValidate(t, r)
	if r.Functions.Emitted.Status != "measured" || *r.Functions.Emitted.Value != 0 {
		t.Fatalf("recovered bodies must be measured zero: %+v", r.Functions.Emitted)
	}
	if r.Functions.Found.Status != "unknown" || r.Functions.Found.Value != nil || r.Functions.Ratio != nil || r.Functions.Comparable {
		t.Fatalf("true native-code denominator must be unknown: %+v", r.Functions)
	}
	if !strings.Contains(r.Functions.Found.Basis, "3 methods (2 with native RVA, 1 without") {
		t.Fatalf("stub method inventory missing from basis: %s", r.Functions.Found.Basis)
	}
	if r.Types.Found.Status != "measured" || *r.Types.Found.Value != 3 || *r.Types.Emitted.Value != 2 || r.Types.Ratio != nil {
		t.Fatalf("types: %+v", r.Types)
	}
	if c := il2cppCheck(t, r, "artifact.fidelity"); c.Status != "pass" || !strings.Contains(c.Message, `"stub"`) {
		t.Fatalf("fidelity: %+v", c)
	}
	if c := il2cppCheck(t, r, "functions.coverage"); c.Status != "warn" {
		t.Fatalf("functions.coverage: %+v", c)
	}
	if c := il2cppCheck(t, r, "provenance.complete"); c.Status != "pass" || !strings.Contains(c.Message, "metadata version 31") {
		t.Fatalf("provenance: %+v", c)
	}
	if c := il2cppCheck(t, r, "output.present"); c.Status != "pass" || !strings.Contains(c.Message, "all 2 metadata images") {
		t.Fatalf("output.present: %+v", c)
	}
	if *r.DecompilerErrors.Value != 0 || r.Status != "degraded" {
		t.Fatalf("status %s errors %+v", r.Status, r.DecompilerErrors)
	}
}

func TestIL2CPPMismatchedMetadataFails(t *testing.T) {
	_, in := il2cppFixture(t, il2cppInspectorHeader)
	in.Target.InputSet[1].SHA256 = il2cppSum("other build metadata")
	r, _ := IL2CPP{}.Collect(context.Background(), in)
	if c := il2cppCheck(t, r, "provenance.complete"); c.Status != "fail" || !strings.Contains(c.Message, "il2cpp-metadata copy sha256") {
		t.Fatalf("mismatch must fail visibly: %+v", c)
	}
	if r.Status != "degraded" {
		t.Fatal(r.Status)
	}
}

func TestIL2CPPParserDriftIsUnknownNotZero(t *testing.T) {
	_, in := il2cppFixture(t, "// some future dumper format\nclass X {}\n")
	r, _ := IL2CPP{}.Collect(context.Background(), in)
	il2cppValidate(t, r)
	if r.Types.Found.Status != "unknown" || r.Types.Found.Value != nil {
		t.Fatalf("drift must be unknown: %+v", r.Types.Found)
	}
}

func TestIL2CPPMissingEvidenceAndPartialDump(t *testing.T) {
	dir, in := il2cppFixture(t, il2cppInspectorHeader+"// Image 2: Extra.dll - Assembly: Extra - Types 10-11\n")
	if err := os.Remove(filepath.Join(dir, "evidence", "il2cpp.json")); err != nil {
		t.Fatal(err)
	}
	r, _ := IL2CPP{}.Collect(context.Background(), in)
	il2cppValidate(t, r)
	if r.DecompilerErrors.Status != "unknown" || r.DecompilerErrors.Value != nil {
		t.Fatalf("missing evidence is not zero errors: %+v", r.DecompilerErrors)
	}
	if c := il2cppCheck(t, r, "provenance.complete"); c.Status != "unknown" {
		t.Fatalf("%+v", c)
	}
	if c := il2cppCheck(t, r, "output.present"); c.Status != "warn" || !strings.Contains(c.Message, "2 of 3 metadata images") {
		t.Fatalf("partial dump must warn: %+v", c)
	}
}

func TestIL2CPPEmptyOutputFails(t *testing.T) {
	r, _ := IL2CPP{}.Collect(context.Background(), Input{Output: t.TempDir()})
	il2cppValidate(t, r)
	if c := il2cppCheck(t, r, "output.present"); c.Status != "fail" {
		t.Fatalf("%+v", c)
	}
	if r.Functions.Emitted.Value != nil {
		t.Fatal("no output must not report a measured zero")
	}
}

func TestParseIL2CPPDumperFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dump.cs")
	il2cppWrite(t, path, "// Image 0: mscorlib.dll - 0\n// Image 1: Game.dll - 4\n\n// Namespace: \npublic class A // TypeDefIndex: 0\n{}\npublic class B // TypeDefIndex: 4\n{}\npublic class C // TypeDefIndex: 6\n{}\n")
	h, err := ParseIL2CPPDump(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if h.Format != "il2cppdumper" || len(h.Images) != 2 || h.Images[0].TypeCount != 4 || h.Images[1].TypeCount != 3 || h.TypeDefCount != 3 {
		t.Fatalf("%+v", h)
	}
}

func TestScanIL2CPPStubsIdentities(t *testing.T) {
	dir, _ := il2cppFixture(t, il2cppInspectorHeader)
	var got []StubMember
	st, err := ScanIL2CPPStubs(context.Background(), filepath.Join(dir, "src"), dir, func(m StubMember) error {
		if m.Kind == "method" {
			got = append(got, m)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if st.Methods != 3 || st.MethodsWithAddress != 2 || st.Types != 2 || len(got) != 3 {
		t.Fatalf("stats %+v rows %d", st, len(got))
	}
	m := got[0]
	if m.Module != "Game.Core.dll" || m.Token != "0x6000001" || m.RVA != "0x7f98f0" || m.VA != "0x1807f98f0" || m.Line != 15 || m.Type != "Player" || m.Path != "src/Game.Core/Game.Core/Player.cs" {
		t.Fatalf("identity %+v", m)
	}
	if got[1].RVA != "" || got[2].Type != "Inner" {
		t.Fatalf("abstract/nested %+v %+v", got[1], got[2])
	}
}

func TestIL2CPPPartialAndFailedAssembliesAreCounted(t *testing.T) {
	dir, in := il2cppFixture(t, il2cppInspectorHeader)
	var ev IL2CPPEvidence
	if err := unityReadJSON(filepath.Join(dir, "evidence", "il2cpp.json"), &ev); err != nil {
		t.Fatal(err)
	}
	ev.Assemblies = append(ev.Assemblies,
		AssemblyResult{Name: "Big.dll", Status: "partial", Mode: "project", Errors: 4},
		AssemblyResult{Name: "Broken.dll", Status: "failed", Mode: "flat", Error: "empty output"})
	b, _ := json.Marshal(ev)
	il2cppWrite(t, filepath.Join(dir, "evidence", "il2cpp.json"), string(b))
	r, _ := IL2CPP{}.Collect(context.Background(), in)
	il2cppValidate(t, r)
	if r.DecompilerErrors.Status != "measured" || *r.DecompilerErrors.Value != 5 {
		t.Fatalf("%+v", r.DecompilerErrors)
	}
	if c := il2cppCheck(t, r, "decompiler.errors"); c.Status != "warn" || !strings.Contains(c.Message, "1 decompiled fully, 1 partial (4 member errors) (Big.dll), 1 failed (Broken.dll)") {
		t.Fatalf("%+v", c)
	}
}
