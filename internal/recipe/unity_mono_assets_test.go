package recipe

import (
	"path/filepath"
	"testing"
)

// TestSharedAssetDir: every assembly of one game resolves to the SAME asset
// export dir (the bug was a per-assembly dir, which re-exported a multi-GB tree
// once per managed DLL), and unusable inputs fall back to the target output.
func TestSharedAssetDir(t *testing.T) {
	root := filepath.Join("E:", "out")
	dataDir := filepath.Join("D:", "Steam", "Valheim", "Valheim_Data")
	want := filepath.Join(root, "_assets", "Valheim_Data")

	a := sharedAssetDir(&Context{Output: filepath.Join(root, "assembly_valheim"), SharedOut: root}, dataDir)
	b := sharedAssetDir(&Context{Output: filepath.Join(root, "UnityEngine.CoreModule"), SharedOut: root}, dataDir)
	if a != want || b != want {
		t.Fatalf("shared dir mismatch: a=%q b=%q want=%q", a, b, want)
	}

	// No SharedOut (direct recipe use): parent of Output stands in for the root.
	out := filepath.Join(root, "assembly_valheim")
	if got := sharedAssetDir(&Context{Output: out}, dataDir); got != want {
		t.Fatalf("fallback root: got %q want %q", got, want)
	}

	// No *_Data dir: keep the old per-target layout.
	if got := sharedAssetDir(&Context{Output: out, SharedOut: root}, ""); got != out {
		t.Fatalf("no game data dir: got %q want %q", got, out)
	}
}
