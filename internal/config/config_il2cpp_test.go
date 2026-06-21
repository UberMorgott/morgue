package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIL2CPPFullExportPersists(t *testing.T) {
	p := filepath.Join(t.TempDir(), "morgue.yaml")
	cfg := Default()
	cfg.IL2CPPFullExport = true
	if err := Save(p, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !loaded.IL2CPPFullExport {
		t.Fatalf("IL2CPPFullExport did not round-trip")
	}
	_ = os.Remove(p)
}

func TestIL2CPPFullExportDefaultsFalse(t *testing.T) {
	if Default().IL2CPPFullExport {
		t.Fatalf("IL2CPPFullExport must default to false (config-only)")
	}
}
