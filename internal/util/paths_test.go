package util

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestLongPathWin covers the pure normalization: only absolute drive/UNC paths
// get the \\?\ prefix, and "." / ".." / duplicate separators (which extended
// paths forbid) are collapsed first. Platform-independent by construction.
func TestLongPathWin(t *testing.T) {
	tests := []struct{ in, want string }{
		{`C:\a\b\file.exe`, `\\?\C:\a\b\file.exe`},
		{`C:\a\.\b\..\c\file.exe`, `\\?\C:\a\c\file.exe`},
		{`C:\a\\b`, `\\?\C:\a\b`},
		{`\\?\C:\a\b`, `\\?\C:\a\b`},                 // already prefixed — untouched
		{`\\.\PhysicalDrive0`, `\\.\PhysicalDrive0`}, // device path — untouched
		{`\\server\share\f.exe`, `\\?\UNC\server\share\f.exe`},
		{`a\b\file.exe`, `a\b\file.exe`}, // relative — left alone
		{`C:file.exe`, `C:file.exe`},     // drive-relative — left alone
		{``, ``},
	}
	for _, tt := range tests {
		if got := longPathWin(tt.in); got != tt.want {
			t.Errorf("longPathWin(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestLongPathIsNoOpOffWindows guards the cross-platform build: on Linux/macOS
// LongPath must never touch the path.
func TestLongPathIsNoOpOffWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows: LongPath rewrites by design")
	}
	for _, p := range []string{"/tmp/x/file.exe", "rel/file.exe", ""} {
		if got := LongPath(p); got != p {
			t.Errorf("LongPath(%q) = %q, want unchanged", p, got)
		}
	}
}

// TestLongPathRoundTrip verifies the rewritten path still opens a real file —
// i.e. short paths keep working after the rewrite.
func TestLongPathRoundTrip(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "probe.bin")
	if err := os.WriteFile(f, []byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}
	lp := LongPath(f)
	if runtime.GOOS == "windows" && !strings.HasPrefix(lp, `\\?\`) {
		t.Fatalf("LongPath(%q) = %q, want \\\\?\\ prefix", f, lp)
	}
	b, err := os.ReadFile(lp)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", lp, err)
	}
	if string(b) != "ok" {
		t.Fatalf("content = %q, want %q", b, "ok")
	}
}

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
