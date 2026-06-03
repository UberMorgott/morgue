package recipe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sampleUsmap builds a small in-memory UsmapData covering the cases the SDK
// dumper must render: a game class with a super and varied property types, an
// engine boilerplate class, a plain struct, and an enum.
func sampleUsmap() *UsmapData {
	return &UsmapData{
		Version:           4,
		CompressionMethod: usmapZStandard,
		Enums: []UsmapEnum{
			{Name: "EMyState", Names: map[uint64]string{0: "Idle", 1: "Running", 2: "EMyState_MAX"}},
		},
		Structs: []UsmapStruct{
			{
				Name:      "MyActor",
				SuperType: "Actor",
				Properties: []UsmapProperty{
					{Index: 0, Name: "Health", ArrayDim: 1, Type: UsmapPropertyType{Type: "FloatProperty"}},
					{Index: 1, Name: "Tags", ArrayDim: 1, Type: UsmapPropertyType{
						Type: "ArrayProperty", Inner: &UsmapPropertyType{Type: "NameProperty"}}},
					{Index: 2, Name: "State", ArrayDim: 1, Type: UsmapPropertyType{
						Type: "EnumProperty", EnumName: "EMyState",
						Inner: &UsmapPropertyType{Type: "ByteProperty"}}},
				},
			},
			{
				Name:      "Actor", // engine boilerplate
				SuperType: "Object",
				Properties: []UsmapProperty{
					{Index: 0, Name: "RootComponent", ArrayDim: 1, Type: UsmapPropertyType{Type: "ObjectProperty"}},
				},
			},
			{
				Name: "MyData", // plain struct (no super)
				Properties: []UsmapProperty{
					{Index: 0, Name: "Id", ArrayDim: 1, Type: UsmapPropertyType{Type: "IntProperty"}},
				},
			},
		},
	}
}

func TestWriteSDKDump(t *testing.T) {
	dir := t.TempDir()
	m := sampleUsmap()

	res, err := writeSDKDump(dir, m, "/fake/Mappings.usmap")
	if err != nil {
		t.Fatalf("writeSDKDump: %v", err)
	}
	if res.Classes != 3 {
		t.Errorf("Classes = %d, want 3", res.Classes)
	}
	if res.Enums != 1 {
		t.Errorf("Enums = %d, want 1", res.Enums)
	}
	if res.Properties != 5 {
		t.Errorf("Properties = %d, want 5", res.Properties)
	}

	// Machine-readable sdk.json must exist and round-trip.
	jsonPath := filepath.Join(dir, "sdk", "sdk.json")
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read sdk.json: %v", err)
	}
	var parsed sdkDump
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("sdk.json invalid: %v", err)
	}
	if len(parsed.Classes) != 3 || len(parsed.Enums) != 1 {
		t.Fatalf("sdk.json class/enum count mismatch: %d/%d", len(parsed.Classes), len(parsed.Enums))
	}

	// Human/AI-readable headers must exist and contain rendered declarations.
	hpp, err := os.ReadFile(filepath.Join(dir, "sdk", "classes.hpp"))
	if err != nil {
		t.Fatalf("read classes.hpp: %v", err)
	}
	hs := string(hpp)
	for _, want := range []string{
		"class MyActor : public Actor",
		"TArray<FName> Tags", // NameProperty renders as the readable FName
		"EMyState State",
		"float Health", // FloatProperty rendered as a C++ type
	} {
		if !strings.Contains(hs, want) {
			t.Errorf("classes.hpp missing %q", want)
		}
	}

	enums, err := os.ReadFile(filepath.Join(dir, "sdk", "enums.hpp"))
	if err != nil {
		t.Fatalf("read enums.hpp: %v", err)
	}
	es := string(enums)
	for _, want := range []string{"enum class EMyState", "Idle = 0", "Running = 1"} {
		if !strings.Contains(es, want) {
			t.Errorf("enums.hpp missing %q", want)
		}
	}

	// README must honestly note the offline source + property-only limitation.
	readme, err := os.ReadFile(filepath.Join(dir, "sdk", "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	rs := string(readme)
	if !strings.Contains(rs, "usmap") {
		t.Error("README should mention usmap source")
	}
	if !strings.Contains(strings.ToLower(rs), "function") {
		t.Error("README should note functions/methods are not present in usmap")
	}

	// Boilerplate flag must be set for the engine class, not the game class.
	var myActor, actor *sdkClass
	for i := range parsed.Classes {
		switch parsed.Classes[i].Name {
		case "MyActor":
			myActor = &parsed.Classes[i]
		case "Actor":
			actor = &parsed.Classes[i]
		}
	}
	if myActor == nil || actor == nil {
		t.Fatal("MyActor/Actor missing from sdk.json")
	}
	if myActor.Boilerplate {
		t.Error("MyActor must not be flagged boilerplate")
	}
	if !actor.Boilerplate {
		t.Error("Actor must be flagged boilerplate")
	}
}

// TestWriteSDKDump_Empty tolerates an empty mapping without error.
func TestWriteSDKDump_Empty(t *testing.T) {
	dir := t.TempDir()
	res, err := writeSDKDump(dir, &UsmapData{Version: 4}, "/fake.usmap")
	if err != nil {
		t.Fatalf("writeSDKDump empty: %v", err)
	}
	if res.Classes != 0 {
		t.Errorf("Classes = %d, want 0", res.Classes)
	}
}
