package tools

import (
	"testing"
)

func TestRegistryNotEmpty(t *testing.T) {
	if len(Registry) == 0 {
		t.Fatal("Registry is empty")
	}
}

func TestFindByName(t *testing.T) {
	tests := []struct {
		name  string
		found bool
	}{
		{"ilspycmd", true},
		{"de4dot-cex", true},
		{"diec", true},
		{"nonexistent", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool, ok := FindByName(tt.name)
			if ok != tt.found {
				t.Errorf("FindByName(%q) found = %v, want %v", tt.name, ok, tt.found)
			}
			if ok && tool.Name != tt.name {
				t.Errorf("FindByName(%q).Name = %q", tt.name, tool.Name)
			}
		})
	}
}

func TestByCategory(t *testing.T) {
	decompilers := ByCategory(CategoryDecompiler)
	if len(decompilers) == 0 {
		t.Error("No decompilers found")
	}

	for _, tool := range decompilers {
		if tool.Category != CategoryDecompiler {
			t.Errorf("ByCategory(Decompiler) returned tool %q with category %v", tool.Name, tool.Category)
		}
	}
}

func TestByCategoryDetector(t *testing.T) {
	detectors := ByCategory(CategoryDetector)
	if len(detectors) == 0 {
		t.Error("No detectors found")
	}
}
