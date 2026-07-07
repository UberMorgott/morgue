package engine

import (
	"testing"
)

// TestFatalMissingTools verifies the Issue 2 fix: a still-missing OPTIONAL tool
// (e.g. ghidra) must NOT be fatal to the task, while a missing REQUIRED tool
// still is.
func TestFatalMissingTools(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{"optional only (ghidra)", []string{"ghidra"}, nil},
		{"optional only (strings)", []string{"strings"}, nil},
		{"required only", []string{"ilspycmd"}, []string{"ilspycmd"}},
		{"mixed", []string{"ghidra", "ilspycmd"}, []string{"ilspycmd"}},
		{"empty", nil, nil},
		{"unknown treated as required", []string{"totally-unknown"}, []string{"totally-unknown"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fatalMissingTools(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("fatalMissingTools(%v) = %v, want %v", tt.input, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("fatalMissingTools(%v)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}
