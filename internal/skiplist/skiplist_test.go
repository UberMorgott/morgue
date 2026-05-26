package skiplist

import (
	"testing"

	"github.com/UberMorgott/morgue/internal/config"
)

func TestShouldSkipSystemLibs(t *testing.T) {
	cfg := config.Default()
	sl := New(cfg)

	tests := []struct {
		name     string
		filename string
		skip     bool
	}{
		{"mscorlib", "mscorlib.dll", true},
		{"System.Runtime", "System.Runtime.dll", true},
		{"kernel32", "kernel32.dll", true},
		{"user app", "MyApp.dll", false},
		{"netstandard", "netstandard.dll", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			skip, _ := sl.ShouldSkip(tt.filename)
			if skip != tt.skip {
				t.Errorf("ShouldSkip(%q) = %v, want %v", tt.filename, skip, tt.skip)
			}
		})
	}
}

func TestShouldSkipDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.SkipSystemLibs = false
	sl := New(cfg)

	skip, _ := sl.ShouldSkip("mscorlib.dll")
	if skip {
		t.Error("ShouldSkip should return false when SkipSystemLibs is disabled")
	}
}

func TestShouldSkipCategoryDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.SkipCategories = map[string]bool{"dotnet_runtime": false}
	sl := New(cfg)

	skip, _ := sl.ShouldSkip("mscorlib.dll")
	if skip {
		t.Error("ShouldSkip should return false when category is disabled")
	}
}

func TestCustomSkip(t *testing.T) {
	cfg := config.Default()
	cfg.CustomSkip = []string{"MySecret.dll"}
	sl := New(cfg)

	skip, cat := sl.ShouldSkip("MySecret.dll")
	if !skip {
		t.Error("ShouldSkip should return true for custom skip")
	}
	if cat != "custom" {
		t.Errorf("category = %q, want custom", cat)
	}
}

func TestCustomInclude(t *testing.T) {
	cfg := config.Default()
	cfg.CustomInclude = []string{"mscorlib.dll"}
	sl := New(cfg)

	skip, _ := sl.ShouldSkip("mscorlib.dll")
	if skip {
		t.Error("ShouldSkip should return false for force-included file")
	}
}
