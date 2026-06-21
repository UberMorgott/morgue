package recipe

import "testing"

func TestInspectorSupportsVersion(t *testing.T) {
	supported := []int32{16, 24, 27, 29, 31, 35, 38, 39, 104, 106}
	for _, v := range supported {
		if !inspectorSupports(v) {
			t.Fatalf("inspector should support v%d", v)
		}
	}
	if inspectorSupports(200) {
		t.Fatalf("inspector should NOT support v200")
	}
}

func TestDumperOrder(t *testing.T) {
	// InspectorRedux available + supports v39 -> inspector first, dumper fallback.
	order := dumperOrder(39, true)
	if len(order) != 2 || order[0] != "il2cppinspector" || order[1] != "il2cppdumper" {
		t.Fatalf("order(v39, avail) = %v", order)
	}
	// InspectorRedux unavailable -> legacy dumper only.
	order = dumperOrder(39, false)
	if len(order) != 1 || order[0] != "il2cppdumper" {
		t.Fatalf("order(v39, !avail) = %v", order)
	}
	// Version inspector can't handle, but available -> dumper first.
	order = dumperOrder(200, true)
	if order[0] != "il2cppdumper" {
		t.Fatalf("order(v200, avail) first = %q, want il2cppdumper", order[0])
	}
}
