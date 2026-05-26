package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/UberMorgott/morgue/internal/config"
	_ "github.com/UberMorgott/morgue/internal/recipe" // trigger init() to register recipes
)

func TestNewEngine(t *testing.T) {
	cfg := config.Default()
	e := New(cfg, t.TempDir())
	if e == nil {
		t.Fatal("New() returned nil")
	}
	if e.tools == nil {
		t.Error("Engine.tools is nil")
	}
	if e.skipList == nil {
		t.Error("Engine.skipList is nil")
	}
}

func TestScanFindsGroups(t *testing.T) {
	root := t.TempDir()

	// Create a Unity Mono layout
	monoDir := filepath.Join(root, "Game_Data", "Managed")
	os.MkdirAll(monoDir, 0755)
	os.WriteFile(filepath.Join(monoDir, "Assembly-CSharp.dll"), []byte("fake"), 0644)

	// Create standalone
	os.WriteFile(filepath.Join(root, "app.exe"), []byte("fake"), 0644)

	cfg := config.Default()
	e := New(cfg, t.TempDir())

	result, err := e.Scan(root)
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}

	if len(result.Files) < 2 {
		t.Errorf("Scan() found %d files, want >= 2", len(result.Files))
	}

	if len(result.Groups) == 0 {
		t.Error("Scan() found no groups")
	}
}

func TestShouldSkip(t *testing.T) {
	cfg := config.Default()
	e := New(cfg, t.TempDir())

	skip, _ := e.ShouldSkip("mscorlib.dll")
	if !skip {
		t.Error("ShouldSkip(mscorlib.dll) should return true")
	}

	skip, _ = e.ShouldSkip("MyApp.dll")
	if skip {
		t.Error("ShouldSkip(MyApp.dll) should return false")
	}
}

func TestToolsManager(t *testing.T) {
	cfg := config.Default()
	e := New(cfg, t.TempDir())

	if e.ToolsManager() == nil {
		t.Error("ToolsManager() returned nil")
	}
}
