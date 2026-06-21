package odin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoldenRoundTrip(t *testing.T) {
	dir := "testdata"
	files := []string{
		"GemComposeAsset.asset",
		"GemSecondDropSheetAsset.asset",
		"GemFirstDropSheetAsset.asset",
		"GemHaveSubSkillSheetAsset.asset",
		"GemDropWeightSheetAsset.asset",
		"GemMainSkillRankDropSheetAsset.asset",
		"GemSkillSheetAsset.asset",
		"GemMainSkillDropSheetAsset.asset",
		"GemSubSkillDropSheetAsset.asset",
	}
	got := DecodeDir(dir, files)

	wantBytes, err := os.ReadFile(filepath.Join(dir, "golden_out.txt"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	want := normalizeNL(string(wantBytes))
	gotN := normalizeNL(got)
	if gotN != want {
		// Emit first divergent line for debugging.
		gl := splitLines(gotN)
		wl := splitLines(want)
		n := len(gl)
		if len(wl) < n {
			n = len(wl)
		}
		for i := 0; i < n; i++ {
			if gl[i] != wl[i] {
				t.Fatalf("line %d differs:\n got=%q\nwant=%q", i+1, gl[i], wl[i])
			}
		}
		t.Fatalf("line count differs: got %d, want %d", len(gl), len(wl))
	}
}

func normalizeNL(s string) string  { return strings.ReplaceAll(s, "\r\n", "\n") }
func splitLines(s string) []string { return strings.Split(s, "\n") }
