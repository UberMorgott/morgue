package recipe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildUsmapResolution_SymbolJoin(t *testing.T) {
	out := t.TempDir()
	srcDir := filepath.Join(out, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Ghidra symbols.json: class names carry the UE U/A prefix; one usmap class
	// ("MyActor") has no symbol; "Helper" matches exactly (no prefix).
	sm := symbolMap{
		Classes: []string{"AMyPawn", "UMyComponent", "Helper", "FUnrelated"},
	}
	data, _ := json.Marshal(&sm)
	if err := os.WriteFile(filepath.Join(srcDir, "symbols.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	m := &UsmapData{
		Version:           4,
		CompressionMethod: usmapZStandard,
		Structs: []UsmapStruct{
			{Name: "MyPawn", SuperType: "Pawn", Properties: []UsmapProperty{
				{Name: "Speed", Type: UsmapPropertyType{Type: "FloatProperty"}}}},
			{Name: "MyComponent", Properties: []UsmapProperty{
				{Name: "Enabled", Type: UsmapPropertyType{Type: "BoolProperty"}}}},
			{Name: "Helper"},  // matches "Helper" exactly
			{Name: "MyActor"}, // no symbol match
			{Name: "Actor"},   // engine boilerplate (matches nothing in symbols)
		},
		Enums: []UsmapEnum{{Name: "EState", Names: map[uint64]string{0: "A"}}},
	}

	ur := buildUsmapResolution(m, srcDir, out)
	if ur.Classes != 5 {
		t.Errorf("Classes = %d, want 5", ur.Classes)
	}
	if ur.Enums != 1 {
		t.Errorf("Enums = %d, want 1", ur.Enums)
	}
	if ur.Properties != 2 {
		t.Errorf("Properties = %d, want 2", ur.Properties)
	}
	if ur.SymbolClassesTotal != 4 {
		t.Errorf("SymbolClassesTotal = %d, want 4", ur.SymbolClassesTotal)
	}
	// MyPawn(AMyPawn), MyComponent(UMyComponent), Helper => 3 matches.
	if ur.MatchedSymbolClasses != 3 {
		t.Errorf("MatchedSymbolClasses = %d, want 3", ur.MatchedSymbolClasses)
	}
	if ur.CompressionMethod != "ZStandard" {
		t.Errorf("CompressionMethod = %q, want ZStandard", ur.CompressionMethod)
	}
	// Sample game classes must exclude engine boilerplate ("Actor" not present
	// anyway) and contain matched game classes.
	foundMyPawn := false
	for _, n := range ur.SampleGameClasses {
		if n == "MyPawn" {
			foundMyPawn = true
		}
	}
	if !foundMyPawn {
		t.Errorf("SampleGameClasses missing MyPawn: %v", ur.SampleGameClasses)
	}
}

func TestBuildUsmapResolution_AssetJoin(t *testing.T) {
	out := t.TempDir()

	// assets_index.json with a sample asset whose name table includes a usmap class.
	ai := assetsIndex{
		Sample: []UAssetInfo{
			{Path: "a.uasset", Names: []string{"MyActor", "SomethingElse"}},
		},
	}
	data, _ := json.Marshal(&ai)
	if err := os.WriteFile(filepath.Join(out, "assets_index.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	m := &UsmapData{
		Version: 4,
		Structs: []UsmapStruct{{Name: "MyActor"}, {Name: "Unseen"}},
	}
	ur := buildUsmapResolution(m, filepath.Join(out, "src"), out)
	if ur.MatchedAssetNames != 1 {
		t.Errorf("MatchedAssetNames = %d, want 1", ur.MatchedAssetNames)
	}
}

func TestStripUEPrefix(t *testing.T) {
	cases := map[string]string{
		"AActor":  "Actor",
		"UObject": "Object",
		"FVector": "Vector",
		"Actor":   "Actor", // no prefix
		"alpha":   "alpha", // lowercase first, unchanged
		"U":       "U",     // too short
	}
	for in, want := range cases {
		if got := stripUEPrefix(in); got != want {
			t.Errorf("stripUEPrefix(%q) = %q, want %q", in, got, want)
		}
	}
	// Crucially, the matcher only strips the EXTERNAL side gated by an exact
	// usmap hit, so a class like "Apple" is NOT joined to a phantom "pple".
	// stripUEPrefix("Apple") does reduce to "pple", but matchUsmap only uses it
	// when "pple" is a real usmap class — verified in TestMatchUsmap_NoPhantom.
}

// TestMatchUsmap_NoPhantom is the dedicated guard for the prefix-strip false
// collision: the usmap set contains "Apple" but NOT "pple". An external "Apple"
// must match "Apple" (exact); an external "pple" must NOT phantom-match anything
// (it would only join if "pple" were a real usmap class, which it isn't).
func TestMatchUsmap_NoPhantom(t *testing.T) {
	out := t.TempDir()
	srcDir := filepath.Join(out, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	// symbols.json carries BOTH the legit name and the phantom-prefix bait.
	sm := symbolMap{Classes: []string{"Apple", "pple"}}
	data, _ := json.Marshal(&sm)
	if err := os.WriteFile(filepath.Join(srcDir, "symbols.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	m := &UsmapData{
		Version: 4,
		Structs: []UsmapStruct{{Name: "Apple"}}, // "pple" is deliberately absent
	}
	ur := buildUsmapResolution(m, srcDir, out)

	// Only "Apple" may match. "pple" must NOT phantom-collide with "Apple" via
	// the A-prefix strip: stripUEPrefix is applied to the EXTERNAL side ("Apple"
	// -> "pple") only when "pple" is a real usmap class — it is not here, so the
	// sole match is the exact "Apple". A buggy two-sided normalize would have
	// matched both and reported 1 group of size 2 / or double-counted.
	if ur.MatchedSymbolClasses != 1 {
		t.Errorf("MatchedSymbolClasses = %d, want 1 (only exact \"Apple\")", ur.MatchedSymbolClasses)
	}
}
