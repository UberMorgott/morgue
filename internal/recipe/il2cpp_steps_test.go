package recipe

import "testing"

func TestIL2CPPStepsIncludeData(t *testing.T) {
	i := &IL2CPP{}
	names := map[string]bool{}
	for _, s := range i.Steps() {
		names[s.Name] = true
	}
	if !names["Extract data layer"] {
		t.Fatalf("expected an 'Extract data layer' step, got %v", names)
	}
	if !names["Decode Odin config"] {
		t.Fatalf("expected a 'Decode Odin config' step, got %v", names)
	}
}

func TestIL2CPPRequiredToolsUpdated(t *testing.T) {
	i := &IL2CPP{}
	want := map[string]bool{}
	for _, n := range i.RequiredTools() {
		want[n] = true
	}
	for _, n := range []string{"il2cppinspector", "il2cppdumper", "ilspycmd", "assetripper"} {
		if !want[n] {
			t.Fatalf("RequiredTools missing %q: %v", n, i.RequiredTools())
		}
	}
}
