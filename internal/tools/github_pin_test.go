package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// resolveInstallTag is the helper that decides which tag to install.
func TestResolveInstallTagPinned(t *testing.T) {
	if got := resolveInstallTag(ToolDef{Version: "2026.2"}, "v9.9.9"); got != "2026.2" {
		t.Fatalf("pinned tag = %q, want 2026.2", got)
	}
	if got := resolveInstallTag(ToolDef{}, "v9.9.9"); got != "v9.9.9" {
		t.Fatalf("unpinned tag = %q, want v9.9.9", got)
	}
}

// TestSelectPrimaryAssetDeterministic: a glob matching multiple assets must yield
// exactly one installable archive, excluding signature/checksum/text sidecars.
func TestSelectPrimaryAssetDeterministic(t *testing.T) {
	matched := []assetInfo{
		{Name: "Tool.CLI-win-x64.zip.sha256", URL: "u1"},
		{Name: "Tool.CLI-win-x64.zip.asc", URL: "u2"},
		{Name: "Tool.CLI-win-x64.zip", URL: "u3"},
		{Name: "RELEASE_NOTES.txt", URL: "u4"},
	}
	got := selectPrimaryAsset(matched)
	if got == nil {
		t.Fatal("selectPrimaryAsset returned nil for a set containing one .zip")
	}
	if got.Name != "Tool.CLI-win-x64.zip" {
		t.Fatalf("selected %q, want the .zip archive", got.Name)
	}
}

func TestSelectPrimaryAssetNoneInstallable(t *testing.T) {
	matched := []assetInfo{
		{Name: "checksums.txt"},
		{Name: "release.json"},
		{Name: "sig.asc"},
	}
	if got := selectPrimaryAsset(matched); got != nil {
		t.Fatalf("expected nil (no installable asset), got %q", got.Name)
	}
	if got := selectPrimaryAsset(nil); got != nil {
		t.Fatalf("expected nil for empty input, got %q", got.Name)
	}
}

func TestValidateBinaryInstalledNested(t *testing.T) {
	dest := t.TempDir()
	// Simulate the InspectorRedux nested layout: destDir/<subdir>/<binary>.
	sub := filepath.Join(dest, "Il2CppInspectorRedux.CLI-win-x64")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	bin := "Il2CppInspector.Redux.CLI.exe"
	if err := os.WriteFile(filepath.Join(sub, bin), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := validateBinaryInstalled(ToolDef{Name: "il2cppinspector", Binary: bin}, dest); err != nil {
		t.Fatalf("nested binary should validate: %v", err)
	}
	// Missing binary must error clearly naming the expected binary.
	err := validateBinaryInstalled(ToolDef{Name: "x", Binary: "Missing.exe"}, dest)
	if err == nil || !strings.Contains(err.Error(), "Missing.exe") {
		t.Fatalf("missing binary should error naming it, got %v", err)
	}
}

// TestPinnedTagNotFoundNoFallback: a pinned install of a non-existent release must
// return a clear "pinned release ... not found" error and must NOT write .version
// (i.e. it must not silently fall through to scrape/latest). Skips if offline.
func TestPinnedTagNotFoundNoFallback(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-touching test in -short mode")
	}
	dest := t.TempDir()
	tool := ToolDef{
		Name:      "bogus-pinned",
		Repo:      "UberMorgott/this-repo-does-not-exist-xyz",
		Version:   "v0.0.0-nope",
		AssetGlob: "*",
		Binary:    "nope.exe",
	}
	_, err := installFromGitHub(tool, dest, "", nil, nil)
	if err == nil {
		t.Fatal("expected error for non-existent pinned release")
	}
	if !strings.Contains(err.Error(), "pinned release") {
		t.Fatalf("error should name the pinned release, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dest, ".version")); statErr == nil {
		t.Fatal(".version written despite pinned failure — silent fallback to latest")
	}
}
