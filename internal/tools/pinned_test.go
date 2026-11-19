package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/UberMorgott/morgue/internal/config"
)

// TestPinnedVersion: a pinned tool reports its own pin as the latest version, so
// the UI never offers an update the installer cannot perform (assetripper was
// pinned at 1.3.14 and still advertised 2.0.0), and never falls back to the
// Last-Modified date of a direct-download asset (one showed "2021.12.07"
// opposite its installed v0.1.0.0-beta).
func TestPinnedVersion(t *testing.T) {
	tests := []struct {
		name   string
		def    ToolDef
		want   string
		pinned bool
	}{
		{"github pin", ToolDef{Version: "1.3.14"}, "1.3.14", true},
		{"direct url pin keeps v stripped", ToolDef{Version: "v0.1.0.0-beta"}, "0.1.0.0-beta", true},
		{"unpinned asks upstream", ToolDef{}, "", false},
		{"nuget DotnetVersion pin", ToolDef{Method: MethodNuGet, DotnetVersion: "11.0.0.9375"}, "11.0.0.9375", true},
		{"dotnet tool DotnetVersion pin", ToolDef{Method: MethodDotnetTool, DotnetVersion: "9.1.0"}, "9.1.0", true},
	}
	for _, tt := range tests {
		got, ok := pinnedVersion(tt.def)
		if got != tt.want || ok != tt.pinned {
			t.Errorf("%s: pinnedVersion() = (%q, %v), want (%q, %v)", tt.name, got, ok, tt.want, tt.pinned)
		}
	}
}

// TestPinnedToolsNeverOfferUpdates guards the registry itself: every pinned tool
// resolves through the pin, so a not-installed (or pin-matching) tool must get
// the pin and updateAvailable=false regardless of what upstream has published.
func TestPinnedToolsNeverOfferUpdates(t *testing.T) {
	m := NewManager(t.TempDir(), config.Config{})
	for _, def := range Registry {
		if def.Version == "" {
			continue
		}
		latest, update := m.CheckLatestVersionSingle(def.Name)
		if want := cleanVersionTag(def.Version); latest != want || update {
			t.Errorf("%s: got (%q, %v), want (%q, false)", def.Name, latest, update, want)
		}
	}
}

// TestPinnedToolOutdatedInstallOffersUpdate: a tool installed at a version other
// than its pin must offer the update (the installer will move it to the pin),
// while an install already at the pin, in any spelling, must not.
func TestPinnedToolOutdatedInstallOffersUpdate(t *testing.T) {
	onlyTestTool(t, ToolDef{Name: "test-pinned", Method: MethodDirectURL, URL: "http://127.0.0.1:1/x.exe", Binary: "x.exe", Version: "v1.3.14"})
	baseDir := t.TempDir()
	dir := filepath.Join(baseDir, "test-pinned")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "x.exe"), []byte("MZ"), 0o644)
	m := NewManager(baseDir, config.Config{})

	for _, tc := range []struct {
		installed string
		update    bool
	}{
		{"1.3.13", true},
		{"2.0.0", true},
		{"1.3.14", false},
		{"v1.3.14.0", false},
		{"latest", false},
	} {
		_ = os.WriteFile(filepath.Join(dir, ".version"), []byte(tc.installed), 0o644)
		latest, update := m.CheckLatestVersionSingle("test-pinned")
		if latest != "1.3.14" || update != tc.update {
			t.Errorf("installed %q: got (%q, %v), want (1.3.14, %v)", tc.installed, latest, update, tc.update)
		}
		for _, st := range m.CheckAllWithUpdates() {
			if st.Name == "test-pinned" && st.UpdateAvailable != tc.update {
				t.Errorf("installed %q: bulk UpdateAvailable = %v, want %v", tc.installed, st.UpdateAvailable, tc.update)
			}
		}
	}
}

func TestIsUpdate(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"1.2.3", "1.2.3", false},
		{"v1.2.3", "1.2.3.0", false},
		{"1.2.3+abc", "1.2.3", false},
		{"1.2.3", "1.2.4", true},
		{"0.1.0.0-beta", "v0.1.0.0-beta", false},
		{"1.0-beta", "1.0-rc1", true},
		{"2025.01.02", "2025.03.04", true},
		{"latest", "1.0", false},
		{"1.0", "–", false},
		{"", "1.0", false},
	} {
		if got := isUpdate(tc.a, tc.b); got != tc.want {
			t.Errorf("isUpdate(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
