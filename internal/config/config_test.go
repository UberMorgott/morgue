package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()

	tests := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"SkipSystemLibs", cfg.SkipSystemLibs, true},
		{"StepTimeoutMinutes", cfg.StepTimeoutMinutes, 60},
		{"ConcurrentTargets", cfg.ConcurrentTargets, 1},
		{"DownloadRetries", cfg.DownloadRetries, 3},
		{"CSharpLanguageVersion", cfg.CSharpLanguageVersion, "Auto"},
		{"LogLevel", cfg.LogLevel, "info"},
		{"UpdateChannel", cfg.UpdateChannel, "stable"},
		{"GenerateCallgraph", cfg.GenerateCallgraph, true},
		{"GenerateDotFiles", cfg.GenerateDotFiles, true},
		{"GeneratePDB", cfg.GeneratePDB, true},
		{"DecompileProjectMode", cfg.DecompileProjectMode, true},
		{"LogToFile", cfg.LogToFile, true},
		{"LogTimestamps", cfg.LogTimestamps, true},
		{"SandboxWarning", cfg.SandboxWarning, true},
		{"AllowDynamicExecution", cfg.AllowDynamicExecution, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("Default().%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := []byte("skip_system_libs: false\nlog_level: debug\nstep_timeout_minutes: 30\n")
	if err := os.WriteFile(cfgPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.SkipSystemLibs != false {
		t.Errorf("SkipSystemLibs = %v, want false", cfg.SkipSystemLibs)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %v, want debug", cfg.LogLevel)
	}
	if cfg.StepTimeoutMinutes != 30 {
		t.Errorf("StepTimeoutMinutes = %v, want 30", cfg.StepTimeoutMinutes)
	}
	// Defaults should still apply for unset fields
	if cfg.ConcurrentTargets != 1 {
		t.Errorf("ConcurrentTargets = %v, want 1 (default)", cfg.ConcurrentTargets)
	}
}

func TestLoadMissing(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("Load() should not error on missing file, got: %v", err)
	}
	// Should return defaults when file doesn't exist
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %v, want info (default)", cfg.LogLevel)
	}
}

func TestSave(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	cfg := Default()
	cfg.LogLevel = "debug"

	if err := Save(cfgPath, cfg); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	loaded, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() after Save() error: %v", err)
	}
	if loaded.LogLevel != "debug" {
		t.Errorf("LogLevel after round-trip = %v, want debug", loaded.LogLevel)
	}
}
