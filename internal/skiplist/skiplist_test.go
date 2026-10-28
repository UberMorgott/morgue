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

// TestShouldSkipSelfContainedDotNetNatives covers the runtime natives a
// self-contained .NET app ships next to its own assemblies (cosmoteer#6a).
func TestShouldSkipSelfContainedDotNetNatives(t *testing.T) {
	sl := New(config.Default())
	for _, name := range []string{
		"clrgc.dll", "clrgcexp.dll", "clretwrc.dll", "clrjit.dll", "coreclr.dll",
		"mscordaccore.dll", "mscordaccore_amd64_amd64_8.0.1124.51707.dll",
		"mscordbi.dll", "mscorrc.dll", "createdump.exe", "msquic.dll",
		"hostfxr.dll", "hostpolicy.dll", "Microsoft.DiaSymReader.Native.amd64.dll",
	} {
		skip, cat := sl.ShouldSkip(name)
		if !skip || cat != "dotnet_bcl" {
			t.Errorf("ShouldSkip(%q) = %v/%q, want true/dotnet_bcl", name, skip, cat)
		}
	}
	if skip, _ := sl.ShouldSkip("Cosmoteer.dll"); skip {
		t.Error("ShouldSkip(Cosmoteer.dll) = true, want false")
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
	cfg.SkipCategories = map[string]bool{"dotnet_bcl": false}
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
