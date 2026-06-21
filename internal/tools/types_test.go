package tools

import "testing"

func TestToolDefVersionField(t *testing.T) {
	td := ToolDef{Name: "x", Version: "v1.2.3"}
	if td.Version != "v1.2.3" {
		t.Fatalf("Version field not set: %q", td.Version)
	}
}
