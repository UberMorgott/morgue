package recipe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestIsBoilerplateClass: known UE/STL engine types are flagged boilerplate;
// game-specific classes are not. Generic prefix list, not Windrose-specific.
func TestIsBoilerplateClass(t *testing.T) {
	boiler := []string{
		"UObject", "AActor", "FName", "FString", "FText",
		"TArray<int>", "TMap<int,FString>", "TSet<int>",
		"FVector", "std::vector<int>", "FArchive",
	}
	game := []string{
		"APlayerCharacter", "UInventoryComponent", "BWeaponSystem",
		"Windrose::Combat", "MyGameMode",
	}
	for _, c := range boiler {
		if !isBoilerplateClass(c) {
			t.Errorf("isBoilerplateClass(%q) = false, want true", c)
		}
	}
	for _, c := range game {
		if isBoilerplateClass(c) {
			t.Errorf("isBoilerplateClass(%q) = true, want false", c)
		}
	}
}

// TestWriteHookable: hookable.json lists named/exported functions (incl
// B3-resolved via name_map overlay) with address+name+signature; anonymous
// FUN_ functions are excluded.
func TestWriteHookable(t *testing.T) {
	srcDir := t.TempDir()
	idx := filepath.Join(srcDir, "indexes")
	if err := os.MkdirAll(idx, 0755); err != nil {
		t.Fatal(err)
	}
	// functions.ndjson: one originally-named, one anonymous-but-resolved, one anon.
	writeFile(t, filepath.Join(srcDir, "functions.ndjson"),
		`{"name":"Already::Named","address":"0x140001000","signature":"void Already::Named(int)","is_named":true}
{"name":"FUN_140002000","address":"0x140002000","signature":"void FUN_140002000(void)","is_named":false}
{"name":"FUN_140003000","address":"0x140003000","signature":"void FUN_140003000(void)","is_named":false}
`)
	// name_map.csv: 140002000 was resolved by B3.
	writeFile(t, filepath.Join(idx, "name_map.csv"),
		"address,old_name,new_name\n0x140002000,FUN_140002000,UPlayer::Jump\n")

	n, err := writeHookable(srcDir)
	if err != nil {
		t.Fatalf("writeHookable: %v", err)
	}
	if n != 2 {
		t.Fatalf("hookable count = %d, want 2 (named + resolved)", n)
	}

	hooks := readHookable(t, filepath.Join(idx, "hookable.json"))
	if hooks["0x140001000"].Name != "Already::Named" {
		t.Errorf("missing originally-named function: %+v", hooks)
	}
	h := hooks["0x140002000"]
	if h.Name != "UPlayer::Jump" {
		t.Errorf("resolved name not applied: got %q", h.Name)
	}
	if h.Signature != "void FUN_140002000(void)" {
		t.Errorf("signature missing/wrong: %q", h.Signature)
	}
	if _, ok := hooks["0x140003000"]; ok {
		t.Errorf("anonymous FUN_ should be excluded from hookable")
	}
}

// TestWriteClassClassification: classes.json tags each recovered class as
// boilerplate or game; nothing is dropped.
func TestWriteClassClassification(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(srcDir, "indexes"), 0755); err != nil {
		t.Fatal(err)
	}
	// symbols.json summary with a Classes list.
	sm := symbolMap{Classes: []string{"UObject", "APlayerCharacter", "TArray<int>", "BWeapon"}}
	data, _ := json.MarshalIndent(&sm, "", "  ")
	writeFile(t, filepath.Join(srcDir, "symbols.json"), string(data))

	total, boiler, err := writeClassClassification(srcDir)
	if err != nil {
		t.Fatalf("writeClassClassification: %v", err)
	}
	if total != 4 {
		t.Fatalf("total classes = %d, want 4 (nothing dropped)", total)
	}
	if boiler != 2 {
		t.Fatalf("boilerplate count = %d, want 2 (UObject, TArray)", boiler)
	}

	rows := readClasses(t, filepath.Join(srcDir, "indexes", "classes.json"))
	if !rows["UObject"] {
		t.Errorf("UObject not flagged boilerplate")
	}
	if rows["APlayerCharacter"] {
		t.Errorf("APlayerCharacter wrongly flagged boilerplate")
	}
}

type hookEntryT struct {
	Address   string `json:"address"`
	Name      string `json:"name"`
	Signature string `json:"signature"`
}

func readHookable(t *testing.T, path string) map[string]hookEntryT {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read hookable.json: %v", err)
	}
	var arr []hookEntryT
	if err := json.Unmarshal(data, &arr); err != nil {
		t.Fatalf("parse hookable.json: %v", err)
	}
	out := map[string]hookEntryT{}
	for _, h := range arr {
		out[h.Address] = h
	}
	return out
}

// readClasses returns map[class]isBoilerplate from classes.json.
func readClasses(t *testing.T, path string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read classes.json: %v", err)
	}
	var arr []struct {
		Name        string `json:"name"`
		Boilerplate bool   `json:"boilerplate"`
	}
	if err := json.Unmarshal(data, &arr); err != nil {
		t.Fatalf("parse classes.json: %v", err)
	}
	out := map[string]bool{}
	for _, c := range arr {
		out[c.Name] = c.Boilerplate
	}
	return out
}
