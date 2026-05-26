package util

import (
	"path/filepath"
	"testing"
)

func TestBaseDir(t *testing.T) {
	dir := BaseDir()
	if dir == "" {
		t.Fatal("BaseDir() returned empty string")
	}
}

func TestToolDir(t *testing.T) {
	dir := ToolDir("ilspy")
	if dir == "" {
		t.Fatal("ToolDir() returned empty string")
	}
	if filepath.Base(dir) != "ilspy" {
		t.Errorf("ToolDir(ilspy) base = %s, want ilspy", filepath.Base(dir))
	}
}

func TestToolPath(t *testing.T) {
	p := ToolPath("ilspy", "ilspycmd.exe")
	if p == "" {
		t.Fatal("ToolPath() returned empty string")
	}
	if filepath.Base(p) != "ilspycmd.exe" {
		t.Errorf("ToolPath base = %s, want ilspycmd.exe", filepath.Base(p))
	}
}

func TestConfigPath(t *testing.T) {
	p := ConfigPath()
	if p == "" {
		t.Fatal("ConfigPath() returned empty string")
	}
	if filepath.Base(p) != "morgue.yaml" {
		t.Errorf("ConfigPath base = %s, want morgue.yaml", filepath.Base(p))
	}
}
