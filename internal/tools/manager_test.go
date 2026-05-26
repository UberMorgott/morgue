package tools

import (
	"testing"
)

func TestCheckEmptyDir(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

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
	mgr := NewManager(dir)

	_, err := mgr.Resolve("nonexistent_tool")
	if err == nil {
		t.Error("Resolve should error for unknown tool")
	}
}

func TestResolveNotInstalled(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

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
	mgr := NewManager(dir)

	statuses := mgr.CheckAll()
	if len(statuses) != len(Registry) {
		t.Errorf("CheckAll returned %d statuses, want %d", len(statuses), len(Registry))
	}
}

func TestToolsNeeded(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	needed := mgr.ToolsNeeded([]string{"ilspycmd", "de4dot-cex"})
	if len(needed) != 2 {
		t.Errorf("ToolsNeeded returned %d, want 2", len(needed))
	}
}

func TestIsInstalled(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	if mgr.IsInstalled("ilspycmd") {
		t.Error("IsInstalled should return false in empty dir")
	}
}
