package recipe

import (
	"os"
	"testing"
)

// TestRealDataFNameSmoke is a lightweight smoke check that parseUAsset returns
// real FName strings from a known asset when the Windrose tree is present.
// Gated on the sample path existing so the suite stays green elsewhere.
func TestRealDataFNameSmoke(t *testing.T) {
	sample := `E:\DEV\Windrose\decompiled\full\pakchunk0-Windows\extracted\00_Paks\Engine\Content\BasicShapes\Cube.uasset`
	if _, err := os.Stat(sample); err != nil {
		t.Skipf("sample asset not present: %v", err)
	}
	info, err := parseUAsset(sample)
	if err != nil {
		t.Fatalf("parseUAsset: %v", err)
	}
	if info.TotalNames == 0 {
		t.Fatalf("Cube.uasset yielded 0 FName strings")
	}
	t.Logf("Cube.uasset names=%v", info.Names)
}
