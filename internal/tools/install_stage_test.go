package tools

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/UberMorgott/morgue/internal/config"
)

func withTestTool(t *testing.T, def ToolDef) {
	t.Helper()
	orig := Registry
	Registry = append(append([]ToolDef(nil), Registry...), def)
	t.Cleanup(func() { Registry = orig })
}

// onlyTestTool makes def the whole registry, so registry-wide calls such as
// CheckAllWithUpdates touch no real upstream.
func onlyTestTool(t *testing.T, def ToolDef) {
	t.Helper()
	orig := Registry
	Registry = []ToolDef{def}
	t.Cleanup(func() { Registry = orig })
}

// TestReinstallFailedDownloadKeepsOldTool: a reinstall (force/update) whose
// download fails must leave the working install untouched.
func TestReinstallFailedDownloadKeepsOldTool(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusInternalServerError)
	}))
	defer srv.Close()
	withTestTool(t, ToolDef{
		Name: "test-stage-tool", Method: MethodDirectURL, URL: srv.URL + "/stage.exe",
		Binary: "stage.exe", Version: "2.0", Optional: true,
	})

	baseDir := t.TempDir()
	toolDir := filepath.Join(baseDir, "test-stage-tool")
	if err := os.MkdirAll(toolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(toolDir, "stage.exe"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(toolDir, ".version"), []byte("1.0"), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(baseDir, config.Config{})
	if _, err := mgr.Install(t.Context(), "test-stage-tool", nil); err == nil {
		t.Fatal("Install succeeded against a failing server")
	}
	data, err := os.ReadFile(filepath.Join(toolDir, "stage.exe")) //nolint:gosec // path is built from this test's own t.TempDir()
	if err != nil || string(data) != "old" {
		t.Fatalf("old binary lost after failed reinstall: %q, %v", data, err)
	}
	if st := mgr.Check("test-stage-tool"); !st.Installed || st.Version != "1.0" {
		t.Fatalf("status after failed reinstall = %+v, want installed 1.0", st)
	}
	if _, err := os.Stat(filepath.Join(baseDir, ".test-stage-tool.staging")); !os.IsNotExist(err) {
		t.Fatalf("staging dir left behind: %v", err)
	}
}

// TestReinstallSwapsInNewVersion: a successful reinstall replaces the old dir.
func TestReinstallSwapsInNewVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("new"))
	}))
	defer srv.Close()
	withTestTool(t, ToolDef{
		Name: "test-stage-tool", Method: MethodDirectURL, URL: srv.URL + "/stage.exe",
		Binary: "stage.exe", Version: "2.0", Optional: true,
	})

	baseDir := t.TempDir()
	toolDir := filepath.Join(baseDir, "test-stage-tool")
	_ = os.MkdirAll(toolDir, 0o755)
	_ = os.WriteFile(filepath.Join(toolDir, "stage.exe"), []byte("old"), 0o644)
	_ = os.WriteFile(filepath.Join(toolDir, "stale.txt"), []byte("x"), 0o644)

	mgr := NewManager(baseDir, config.Config{})
	ver, err := mgr.Install(t.Context(), "test-stage-tool", nil)
	if err != nil || ver != "2.0" {
		t.Fatalf("Install = (%q, %v)", ver, err)
	}
	if data, _ := os.ReadFile(filepath.Join(toolDir, "stage.exe")); string(data) != "new" { //nolint:gosec // path is built from this test's own t.TempDir()
		t.Fatalf("binary not replaced: %q", data)
	}
	if _, err := os.Stat(filepath.Join(toolDir, "stale.txt")); !os.IsNotExist(err) {
		t.Fatal("old install contents survived the swap")
	}
	if _, err := os.Stat(filepath.Join(baseDir, ".test-stage-tool.old")); !os.IsNotExist(err) {
		t.Fatal("backup dir left behind")
	}
}
