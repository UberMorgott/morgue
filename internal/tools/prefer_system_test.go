package tools

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/UberMorgott/morgue/internal/config"
)

// TestPreferSystemTools: with prefer_system_tools the PATH copy wins (ilspycmd
// via its global-tool shim name), is reported as "system", and never offers an
// update; with the flag off PATH is ignored.
func TestPreferSystemTools(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("shim lookup relies on PATHEXT")
	}
	bin := t.TempDir()
	shim := filepath.Join(bin, "ilspycmd.exe")
	if err := os.WriteFile(shim, []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)

	off := NewManager(t.TempDir(), config.Config{})
	if p, err := off.Resolve("ilspycmd"); err == nil {
		t.Fatalf("flag off resolved %s from PATH", p)
	}

	on := NewManager(t.TempDir(), config.Config{PreferSystemTools: true})
	p, err := on.Resolve("ilspycmd")
	if err != nil || !strings.EqualFold(p, shim) {
		t.Fatalf("Resolve = (%q, %v), want %s", p, err, shim)
	}
	st := on.Check("ilspycmd")
	if !st.Installed || st.Version != systemVersion || !strings.EqualFold(st.Path, shim) {
		t.Fatalf("Check = %+v", st)
	}
	if on.ToolsNeeded([]string{"ilspycmd"}) != nil {
		t.Fatal("system tool still scheduled for download")
	}

	// Pinned so the update check stays offline.
	onlyTestTool(t, ToolDef{Name: "test-sys", Method: MethodDirectURL, URL: "http://127.0.0.1:1/s.exe", Binary: "s.exe", Version: "9.9"})
	_ = os.WriteFile(filepath.Join(bin, "s.exe"), []byte("MZ"), 0o755)
	if _, upd := on.CheckLatestVersionSingle("test-sys"); upd {
		t.Fatal("system tool offered a managed update")
	}
	// Non-executable binaries are never taken from PATH.
	if got := on.systemToolPath(ToolDef{Binary: "x.jar"}); got != "" {
		t.Fatalf("jar resolved from PATH: %s", got)
	}
}
