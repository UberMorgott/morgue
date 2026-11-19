package recipe

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSharedAssetDir: every assembly of one game resolves to the SAME asset
// export dir (the bug was a per-assembly dir, which re-exported a multi-GB tree
// once per managed DLL), and unusable inputs fall back to the target output.
func TestSharedAssetDir(t *testing.T) {
	root := filepath.Join("E:", "out")
	dataDir := filepath.Join("D:", "Steam", "Valheim", "Valheim_Data")
	wantPrefix := filepath.Join(root, "_assets", "Valheim_Data-")

	a := sharedAssetDir(&Context{Output: filepath.Join(root, "assembly_valheim"), SharedOut: root}, dataDir)
	b := sharedAssetDir(&Context{Output: filepath.Join(root, "UnityEngine.CoreModule"), SharedOut: root}, dataDir)
	if a != b || !strings.HasPrefix(a, wantPrefix) {
		t.Fatalf("shared dir mismatch: a=%q b=%q want=%q", a, b, wantPrefix)
	}

	// Equal basenames in different games must not share products.
	if other := sharedAssetDir(&Context{Output: filepath.Join(root, "other"), SharedOut: root}, filepath.Join("D:", "Other", "Valheim_Data")); other == a {
		t.Fatal("different games collided")
	}

	// No SharedOut (direct recipe use): parent of Output stands in for the root.
	out := filepath.Join(root, "assembly_valheim")
	if got := sharedAssetDir(&Context{Output: out}, dataDir); got != a {
		t.Fatalf("fallback root: got %q want %q", got, a)
	}

	// No *_Data dir: keep the old per-target layout.
	if got := sharedAssetDir(&Context{Output: out, SharedOut: root}, ""); got != out {
		t.Fatalf("no game data dir: got %q want %q", got, out)
	}
}

func TestSharedProductIgnoresOnDiskMarkersWithoutLiveRun(t *testing.T) {
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, ".ripped"), []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	ctx := &Context{Ctx: context.Background()}
	for range 2 {
		reused, err := runSharedProduct(ctx, "same-key", out, "asset", "extract", "assetripper", func() error { calls++; return os.WriteFile(filepath.Join(out, "fresh.txt"), []byte("fresh"), 0600) })
		if err != nil || reused {
			t.Fatalf("reused old marker: %v / %v", reused, err)
		}
	}
	if calls != 2 {
		t.Fatalf("without a live cache, marker suppressed work: %d", calls)
	}
}
