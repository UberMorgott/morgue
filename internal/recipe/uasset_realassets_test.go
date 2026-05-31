package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRealAssetsFNameExtraction validates the .uasset parser against real
// extracted Windrose assets when present. Gated on the path existing so the
// suite stays green on machines without the game tree.
func TestRealAssetsFNameExtraction(t *testing.T) {
	root := `E:\DEV\Windrose\decompiled\full\pakchunk0-Windows\extracted`
	if _, err := os.Stat(root); err != nil {
		t.Skipf("real assets not present: %v", err)
	}
	var checked, withNames int
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || checked >= 50 {
			return nil
		}
		if strings.ToLower(filepath.Ext(p)) != ".uasset" {
			return nil
		}
		checked++
		info, perr := parseUAsset(p)
		if perr == nil && info != nil && info.TotalNames > 0 {
			withNames++
		}
		return nil
	})
	if checked > 0 && withNames == 0 {
		t.Fatalf("parsed %d real assets, none yielded FName strings", checked)
	}
	t.Logf("checked=%d withNames=%d", checked, withNames)
}
