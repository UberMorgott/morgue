package gamedata

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// readTestFile reads a file the test itself produced under t.TempDir().
func readTestFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // path is built from this test's own t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// asMapT asserts that a decoded JSON value is an object.
func asMapT(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("want JSON object, got %T (%v)", v, v)
	}
	return m
}

// assetNone builds a plain Unity-serialized (non-Odin) MonoBehaviour asset.
func assetNone(name, guid, extra string) string {
	return "%YAML 1.1\n" +
		"%TAG !u! tag:unity3d.com,2011:\n" +
		"--- !u!114 &11400000\n" +
		"MonoBehaviour:\n" +
		"  m_Name: " + name + "\n" +
		"  m_Script: {fileID: 11500000, guid: " + guid + ", type: 3}\n" +
		extra
}

// --- scripts.go ---

func TestParseGuidFromMeta(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "Foo.cs.meta")
	writeTestFile(t, p, "fileFormatVersion: 2\nguid: 1234567890abcdef1234567890abcdef\nMonoImporter:\n")
	if got := parseGuidFromMeta(p); got != "1234567890abcdef1234567890abcdef" {
		t.Fatalf("guid = %q", got)
	}
}

func TestBuildScriptMap(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "Assets", "Scripts", "WeaponDef.cs.meta"),
		"guid: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n")
	writeTestFile(t, filepath.Join(dir, "Assets", "Scripts", "Sub", "InputMapDef.cs.meta"),
		"guid: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n")
	// A non-script .meta must be ignored.
	writeTestFile(t, filepath.Join(dir, "Assets", "tex.png.meta"), "guid: cccc\n")

	m := buildScriptMap(Options{ExportDir: dir})
	if m["aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"] != "WeaponDef" {
		t.Fatalf("WeaponDef mapping missing: %v", m)
	}
	if m["bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"] != "InputMapDef" {
		t.Fatalf("InputMapDef mapping missing: %v", m)
	}
	if len(m) != 2 {
		t.Fatalf("expected 2 mappings, got %d: %v", len(m), m)
	}
}

// --- defs.go grouping ---

func TestOrganizeDefsGrouping(t *testing.T) {
	export := t.TempDir()
	out := t.TempDir()
	guid := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	writeTestFile(t, filepath.Join(export, "Assets", "Scripts", "WeaponDef.cs.meta"), "guid: "+guid+"\n")
	writeTestFile(t, filepath.Join(export, "Assets", "Defs", "Weapon1.asset"), assetNone("Weapon1", guid, "  Damage: 5\n"))
	writeTestFile(t, filepath.Join(export, "Assets", "Defs", "Weapon2.asset"), assetNone("Weapon2", guid, "  Damage: 9\n"))
	// Unknown-type asset (guid not in script map) -> UnknownType bucket.
	writeTestFile(t, filepath.Join(export, "Assets", "Defs", "Mystery.asset"), assetNone("Mystery", "ffffffffffffffffffffffffffffffff", ""))

	scriptMap := buildScriptMap(Options{ExportDir: export})
	res := organizeDefs(Options{ExportDir: export, OutDir: out}, scriptMap)

	if res.Counts["WeaponDef"] != 2 {
		t.Fatalf("WeaponDef count = %d, want 2 (%v)", res.Counts["WeaponDef"], res.Counts)
	}
	if res.Counts["UnknownType"] != 1 {
		t.Fatalf("UnknownType count = %d, want 1", res.Counts["UnknownType"])
	}
	for _, f := range []string{"Weapon1.json", "Weapon2.json"} {
		p := filepath.Join(out, "defs", "WeaponDef", f)
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected %s: %v", p, err)
		}
	}
	// Verify the record shape of one written def.
	data := readTestFile(t, filepath.Join(out, "defs", "WeaponDef", "Weapon1.json"))
	var rec defRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.TypeName != "WeaponDef" || rec.DefName != "Weapon1" || rec.OdinFormat != "none" {
		t.Fatalf("rec = %+v", rec)
	}
	if rec.Guid != guid {
		t.Fatalf("rec.Guid = %q", rec.Guid)
	}
}

// --- inputmap.go rendering ---

// inputMapFixture mirrors the real Phoenix Point InputMapDef shape:
// Action{Name, ActionSection, ActionDisplayText{LocalizationKey}, Chords[]} /
// Chord{OverridingBehavior, Keys[]} / Key{Name, InputSource, DeadzoneOverride}.
func inputMapFixture() map[string]any {
	return map[string]any{
		"Actions": []any{
			map[string]any{
				"Name":              "Change Level",
				"ActionSection":     int64(0),
				"ActionDisplayText": map[string]any{"LocalizationKey": "KEY_CHANGE_LEVEL"},
				"Chords": []any{
					map[string]any{
						"OverridingBehavior": int64(2),
						"Keys": []any{
							map[string]any{"Name": "z", "InputSource": int64(0), "DeadzoneOverride": int64(-1)},
						},
					},
				},
			},
			map[string]any{
				"Name":          "Scroll Zoom",
				"ActionSection": int64(1),
				"Chords": []any{
					map[string]any{
						"OverridingBehavior": int64(0),
						"Keys": []any{
							map[string]any{"Name": "ScrollWheel", "InputSource": int64(3), "DeadzoneOverride": float64(0.1)},
						},
					},
				},
			},
		},
	}
}

func TestRenderInputMap(t *testing.T) {
	md, total := renderInputMap([]map[string]any{inputMapFixture()})
	if total != 2 {
		t.Fatalf("action count = %d, want 2", total)
	}
	wants := []string{
		"| Change Level | Tactical | [Allowed] z : Key : dz=-1 |",
		"| Scroll Zoom | Geoscape | [Hidden] ScrollWheel : Axis : dz=0.1 |",
	}
	for _, w := range wants {
		if !strings.Contains(md, w) {
			t.Fatalf("md missing row %q\n---\n%s", w, md)
		}
	}
}

func TestOrganizeInputWritesFiles(t *testing.T) {
	out := t.TempDir()
	n := organizeInput(Options{OutDir: out}, []map[string]any{inputMapFixture()})
	if n != 2 {
		t.Fatalf("count = %d, want 2", n)
	}
	for _, f := range []string{"inputmap.md", "inputmap.json"} {
		if _, err := os.Stat(filepath.Join(out, "input", f)); err != nil {
			t.Fatalf("missing %s: %v", f, err)
		}
	}
}

// --- loc.go ---

func TestOrganizeLoc(t *testing.T) {
	export := t.TempDir()
	out := t.TempDir()
	writeTestFile(t, filepath.Join(export, "Assets", "Loc", "strings.csv"),
		"Keys,English,Russian\nGREETING,Hello,Privet\nFAREWELL,Bye,Poka\n")
	// A non-loc CSV (wrong header) must be ignored.
	writeTestFile(t, filepath.Join(export, "Assets", "other.csv"), "Col1,Col2\na,b\n")

	rep := &Report{}
	counts := organizeLoc(Options{ExportDir: export, OutDir: out}, nil, rep)
	if counts["English"] != 2 || counts["Russian"] != 2 {
		t.Fatalf("counts = %v, want English=2 Russian=2", counts)
	}
	data := readTestFile(t, filepath.Join(out, "loc", "English.json"))
	var kv map[string]string
	if err := json.Unmarshal(data, &kv); err != nil {
		t.Fatal(err)
	}
	if kv["GREETING"] != "Hello" || kv["FAREWELL"] != "Bye" {
		t.Fatalf("English loc = %v", kv)
	}
}

// --- refs.go ---

func TestResolveRefs(t *testing.T) {
	defsDir := t.TempDir()
	rec := map[string]any{
		"typeName": "ADef",
		"fields": map[string]any{
			"link":     map[string]any{"$ref": "guid-xyz"},
			"intra":    map[string]any{"$ref": "#3"},
			"unknown":  map[string]any{"$ref": "missing"},
			"deeplist": []any{map[string]any{"$ref": "guid-xyz"}},
		},
	}
	if err := writeJSON(filepath.Join(defsDir, "AAA", "a.json"), rec); err != nil {
		t.Fatal(err)
	}
	index := map[string]string{"guid-xyz": "TargetDef"}
	if err := resolveRefs(defsDir, index); err != nil {
		t.Fatal(err)
	}
	data := readTestFile(t, filepath.Join(defsDir, "AAA", "a.json"))
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	fields := asMapT(t, got["fields"])
	if name := asMapT(t, fields["link"])["$name"]; name != "TargetDef" {
		t.Fatalf("link $name = %v, want TargetDef", name)
	}
	if name, ok := asMapT(t, fields["unknown"])["$name"]; !ok || name != nil {
		t.Fatalf("unknown $name = %v, want null present", name)
	}
	deeplist, ok := fields["deeplist"].([]any)
	if !ok || len(deeplist) == 0 {
		t.Fatalf("deeplist = %v, want a non-empty array", fields["deeplist"])
	}
	if name := asMapT(t, deeplist[0])["$name"]; name != "TargetDef" {
		t.Fatalf("deeplist $name = %v, want TargetDef", name)
	}
}

// I2 localization source path (real Phoenix Point shape: mSource.mLanguages
// index-aligned with each mTerms[].Languages).
func TestOrganizeLocI2(t *testing.T) {
	out := t.TempDir()
	i2 := map[string]any{
		"mSource": map[string]any{
			"mLanguages": []any{
				map[string]any{"Name": "English", "Code": "en"},
				map[string]any{"Name": "Russian", "Code": "ru"},
				map[string]any{"Name": "UI_Tester", "Code": ""}, // empty code -> falls back to Name
			},
			"mTerms": []any{
				map[string]any{"Term": "Cat/KEY_HELLO", "Key": "KEY_HELLO", "Languages": []any{"Hello", "Privet", nil}},
				map[string]any{"Term": "Cat/KEY_BYE", "Key": "KEY_BYE", "Languages": []any{"Bye", "Poka", nil}},
			},
		},
	}
	rep := &Report{}
	counts := organizeLoc(Options{OutDir: out}, []map[string]any{i2}, rep)
	if counts["en"] != 2 || counts["ru"] != 2 {
		t.Fatalf("counts = %v, want en=2 ru=2", counts)
	}
	if counts["UI_Tester"] != 2 {
		t.Fatalf("UI_Tester count = %d (empty-code fallback to Name)", counts["UI_Tester"])
	}
	data := readTestFile(t, filepath.Join(out, "loc", "ru.json"))
	var kv map[string]string
	if err := json.Unmarshal(data, &kv); err != nil {
		t.Fatal(err)
	}
	if kv["Cat/KEY_HELLO"] != "Privet" || kv["Cat/KEY_BYE"] != "Poka" {
		t.Fatalf("ru loc = %v", kv)
	}
}

// Plain Unity {fileID, guid, type} object references must resolve to $name.
func TestResolveUnityRefs(t *testing.T) {
	defsDir := t.TempDir()
	rec := map[string]any{
		"fields": map[string]any{
			"ViewElementDef": map[string]any{"fileID": float64(11400000), "guid": "target-guid", "type": float64(2)},
			"Missing":        map[string]any{"fileID": float64(11400000), "guid": "no-such", "type": float64(2)},
			"Null":           map[string]any{"fileID": float64(0)},
		},
	}
	if err := writeJSON(filepath.Join(defsDir, "W", "w.json"), rec); err != nil {
		t.Fatal(err)
	}
	if err := resolveRefs(defsDir, map[string]string{"target-guid": "SomeViewDef"}); err != nil {
		t.Fatal(err)
	}
	data := readTestFile(t, filepath.Join(defsDir, "W", "w.json"))
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	f := asMapT(t, got["fields"])
	if name := asMapT(t, f["ViewElementDef"])["$name"]; name != "SomeViewDef" {
		t.Fatalf("ViewElementDef $name = %v, want SomeViewDef", name)
	}
	// Unresolved guid ref stays untouched (no $name) to avoid mass churn.
	if _, ok := asMapT(t, f["Missing"])["$name"]; ok {
		t.Fatalf("unresolved ref should not gain $name")
	}
	// Null ref (no guid) untouched.
	if _, ok := asMapT(t, f["Null"])["$name"]; ok {
		t.Fatalf("null ref should not gain $name")
	}
}

// --- inventory.go (streaming XML) ---

func TestOrganizeInventoryXML(t *testing.T) {
	dir := t.TempDir()
	out := t.TempDir()
	xmlDoc := `<?xml version="1.0" encoding="utf-8"?>
<Assets filename="x.assets" createdAt="2026-06-26">
  <Asset>
    <Name>Plain Texture</Name>
    <Container>assets/tex.png</Container>
    <Type id="28">Texture2D</Type>
    <PathID>123</PathID>
    <Source>D:\game\data.assets</Source>
    <TreeNode>scene/root</TreeNode>
    <Size>4096</Size>
  </Asset>
  <Asset>
    <Name>Weapon, Heavy "Gun"</Name>
    <Container>assets/w.prefab</Container>
    <Type id="1">GameObject</Type>
    <PathID>456</PathID>
    <Source>D:\game\data.assets</Source>
    <Size>200</Size>
  </Asset>
  <Asset>
    <Name>Another Texture</Name>
    <Type id="28">Texture2D</Type>
    <PathID>789</PathID>
    <Source>D:\game\b.bundle</Source>
    <Size>10</Size>
  </Asset>
</Assets>`
	inPath := filepath.Join(dir, "assets.xml")
	if err := os.WriteFile(inPath, []byte(xmlDoc), 0o644); err != nil {
		t.Fatal(err)
	}

	rep := &Report{AssetCountsByType: map[string]int{}}
	organizeInventory(Options{InventoryCSV: inPath, OutDir: out}, rep)

	f, err := os.Open(filepath.Join(out, "inventory", "assets.csv")) //nolint:gosec // path is built from this test's own t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("read back csv: %v", err)
	}
	// The GameObject asset is scene-graph bulk: counted but NOT a CSV row, so the
	// CSV is header + the 2 Texture2D content rows only.
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (header + 2 content): %v", len(rows), rows)
	}
	wantHeader := []string{"Name", "Type", "Container", "PathID", "SourceBundle", "Size"}
	for i, h := range wantHeader {
		if rows[0][i] != h {
			t.Fatalf("header[%d] = %q, want %q", i, rows[0][i], h)
		}
	}
	// Both data rows must be content (Texture2D); no GameObject row may survive.
	for _, r := range rows[1:] {
		if r[1] == "GameObject" {
			t.Fatalf("GameObject row leaked into content CSV: %v", r)
		}
	}
	// Type comes from the element text, not the id attribute.
	if rows[1][1] != "Texture2D" || rows[2][1] != "Texture2D" {
		t.Fatalf("content rows = %v", rows)
	}
	// Counts stay COMPLETE: every type, including the filtered-out GameObject.
	if rep.AssetCountsByType["Texture2D"] != 2 || rep.AssetCountsByType["GameObject"] != 1 {
		t.Fatalf("AssetCountsByType = %v", rep.AssetCountsByType)
	}
}

// --- end-to-end ---

func TestOrganizeEndToEnd(t *testing.T) {
	export := t.TempDir()
	out := filepath.Join(t.TempDir(), "GameData")
	guid := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	writeTestFile(t, filepath.Join(export, "Assets", "Scripts", "WeaponDef.cs.meta"), "guid: "+guid+"\n")
	writeTestFile(t, filepath.Join(export, "Assets", "Defs", "Weapon1.asset"), assetNone("Weapon1", guid, "  Damage: 5\n"))
	writeTestFile(t, filepath.Join(export, "Assets", "Loc", "strings.csv"),
		"Keys,English\nGREETING,Hello\n")

	rep, err := Organize(Options{
		ExportDir:    export,
		OutDir:       out,
		UnityVersion: "2019.4.31f1",
		ToolVersions: map[string]string{"assetripper": "1.0"},
	})
	if err != nil {
		t.Fatalf("Organize: %v", err)
	}
	if rep.DefCountsByType["WeaponDef"] != 1 {
		t.Fatalf("DefCountsByType = %v", rep.DefCountsByType)
	}
	if rep.LocKeyCounts["English"] != 1 {
		t.Fatalf("LocKeyCounts = %v", rep.LocKeyCounts)
	}
	for _, f := range []string{"INDEX.md", "manifest.json", "README.md"} {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			t.Fatalf("missing %s: %v", f, err)
		}
	}
	// manifest carries the unity version + extraction date.
	data := readTestFile(t, filepath.Join(out, "manifest.json"))
	if !strings.Contains(string(data), "2019.4.31f1") {
		t.Fatalf("manifest missing unity version:\n%s", data)
	}
}
