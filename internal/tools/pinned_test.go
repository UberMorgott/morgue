package tools

import (
	"testing"

	"github.com/UberMorgott/morgue/internal/config"
)

// TestPinnedVersion: a pinned tool reports its own pin as the latest version, so
// the UI never offers an update the installer cannot perform (assetripper was
// pinned at 1.3.14 and still advertised 2.0.0), and never falls back to the
// Last-Modified date of a direct-download asset (confuserex-killer showed
// "2021.12.07" opposite its installed v0.1.0.0-beta).
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
	}
	for _, tt := range tests {
		got, ok := pinnedVersion(tt.def)
		if got != tt.want || ok != tt.pinned {
			t.Errorf("%s: pinnedVersion() = (%q, %v), want (%q, %v)", tt.name, got, ok, tt.want, tt.pinned)
		}
	}
}

// TestPinnedToolsNeverOfferUpdates guards the registry itself: every pinned tool
// resolves through the pin, so CheckLatestVersionSingle must answer with the pin
// and updateAvailable=false regardless of what upstream has published.
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
