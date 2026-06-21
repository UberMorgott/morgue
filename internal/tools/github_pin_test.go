package tools

import "testing"

// resolveInstallTag is the helper that decides which tag to install.
func TestResolveInstallTagPinned(t *testing.T) {
	if got := resolveInstallTag(ToolDef{Version: "2026.2"}, "v9.9.9"); got != "2026.2" {
		t.Fatalf("pinned tag = %q, want 2026.2", got)
	}
	if got := resolveInstallTag(ToolDef{}, "v9.9.9"); got != "v9.9.9" {
		t.Fatalf("unpinned tag = %q, want v9.9.9", got)
	}
}
