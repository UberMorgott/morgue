package recipe

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPhaseBLayout proves a split followed by the Phase B enrichment passes
// produces the full navigable layout the design spec calls for: functions/
// buckets, symbols.json + symbols.ndjson, and indexes/{string_refs.csv,
// callers.csv, name_map.csv, classes.json, hookable.json}. This is the
// integration contract wired into ue5.go/native.go.
func TestPhaseBLayout(t *testing.T) {
	srcDir := t.TempDir()
	binPath := filepath.Join(srcDir, "game.exe")
	combined := filepath.Join(srcDir, "game.c")
	body := `// Decompiled by Ghidra via Morgue
// Binary: game.exe
// Architecture: x86:LE:64:default
// 140001000
void Engine::Widget::Tick(void)
{
  puts("alpha");
  FUN_140002000();
}
// 140002000
void FUN_140002000(void)
{
  iVar1 = UPlayer::Jump("UPlayer::Jump");
  vinsertps_avx(0);
}
`
	if err := os.WriteFile(combined, []byte(body), 0644); err != nil {
		t.Fatalf("write combined: %v", err)
	}

	if _, err := splitAndIndexDecompiledC(srcDir, binPath); err != nil {
		t.Fatalf("split: %v", err)
	}
	if _, err := resolveNames(srcDir); err != nil {
		t.Fatalf("resolveNames: %v", err)
	}
	if _, _, err := writeClassClassification(srcDir); err != nil {
		t.Fatalf("writeClassClassification: %v", err)
	}
	if _, err := writeHookable(srcDir); err != nil {
		t.Fatalf("writeHookable: %v", err)
	}

	// Required top-level artifacts.
	for _, rel := range []string{
		"symbols.json",
		"symbols.ndjson",
		"functions.ndjson",
		"functions_index.json",
		filepath.Join("indexes", "string_refs.csv"),
		filepath.Join("indexes", "callers.csv"),
		filepath.Join("indexes", "name_map.csv"),
		filepath.Join("indexes", "classes.json"),
		filepath.Join("indexes", "hookable.json"),
	} {
		p := filepath.Join(srcDir, rel)
		if st, err := os.Stat(p); err != nil || st.Size() == 0 {
			t.Errorf("missing or empty artifact: %s (err=%v)", rel, err)
		}
	}

	// functions/ must have at least one bucket .c file.
	if countBucketFiles(t, filepath.Join(srcDir, "functions")) < 1 {
		t.Errorf("no bucket files produced under functions/")
	}
}
