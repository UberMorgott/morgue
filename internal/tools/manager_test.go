package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/UberMorgott/morgue/internal/config"
)

func TestCheckEmptyDir(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir, config.Config{})

	status := mgr.Check("ilspycmd")
	if status.Installed {
		t.Error("Check should return not installed for empty dir")
	}
	if status.Name != "ilspycmd" {
		t.Errorf("Check name = %q, want ilspycmd", status.Name)
	}
}

func TestResolveUnknownTool(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir, config.Config{})

	_, err := mgr.Resolve("nonexistent_tool")
	if err == nil {
		t.Error("Resolve should error for unknown tool")
	}
}

func TestResolveNotInstalled(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir, config.Config{})

	path, err := mgr.Resolve("ilspycmd")
	if err == nil {
		t.Error("Resolve should error when tool is not installed")
	}
	if path != "" {
		t.Errorf("Resolve path = %q, want empty", path)
	}
}

func TestCheckAll(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir, config.Config{})

	statuses := mgr.CheckAll()
	if len(statuses) != len(Registry) {
		t.Errorf("CheckAll returned %d statuses, want %d", len(statuses), len(Registry))
	}
}

func TestResolveGhidraFromEnv(t *testing.T) {
	// A pre-seeded, offline Ghidra home containing ghidraRun.bat must be usable
	// via $GHIDRA_HOME with no managed install and no network.
	home := t.TempDir()
	runBat := filepath.Join(home, "ghidraRun.bat")
	if err := os.WriteFile(runBat, []byte("@echo ghidra"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GHIDRA_HOME", home)

	// Manager base dir is empty — no managed ghidra present.
	mgr := NewManager(t.TempDir(), config.Config{})

	got, err := mgr.Resolve("ghidra")
	if err != nil {
		t.Fatalf("Resolve(ghidra) with GHIDRA_HOME should succeed, got: %v", err)
	}
	if got != runBat {
		t.Errorf("Resolve(ghidra) = %q, want %q", got, runBat)
	}
	if !mgr.IsInstalled("ghidra") {
		t.Error("IsInstalled(ghidra) should be true when GHIDRA_HOME is set")
	}
}

func TestResolveGhidraFromConfig(t *testing.T) {
	// support/analyzeHeadless.bat is an acceptable marker for a Ghidra home.
	home := t.TempDir()
	support := filepath.Join(home, "support")
	if err := os.MkdirAll(support, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(support, "analyzeHeadless.bat"), []byte("@echo hl"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GHIDRA_HOME", "")

	mgr := NewManager(t.TempDir(), config.Config{GhidraHome: home})
	got, err := mgr.Resolve("ghidra")
	if err != nil {
		t.Fatalf("Resolve(ghidra) with cfg.GhidraHome should succeed, got: %v", err)
	}
	want := filepath.Join(home, "ghidraRun.bat")
	if got != want {
		t.Errorf("Resolve(ghidra) = %q, want %q", got, want)
	}
}

func TestToolsNeeded(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir, config.Config{})

	needed := mgr.ToolsNeeded([]string{"ilspycmd", "de4dot-cex"})
	if len(needed) != 2 {
		t.Errorf("ToolsNeeded returned %d, want 2", len(needed))
	}
}

func TestIsInstalled(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir, config.Config{})

	if mgr.IsInstalled("ilspycmd") {
		t.Error("IsInstalled should return false in empty dir")
	}
}
