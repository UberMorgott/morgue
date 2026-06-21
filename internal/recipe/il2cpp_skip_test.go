package recipe

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStageDoneSkip(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "dump", "cs", "il2cpp.cs")
	os.MkdirAll(filepath.Dir(marker), 0755)
	os.WriteFile(marker, []byte("x"), 0644)

	if !stageDone(marker, false) {
		t.Fatalf("stageDone should be true when marker exists and !force")
	}
	if stageDone(marker, true) {
		t.Fatalf("stageDone must be false when force=true")
	}
	if stageDone(filepath.Join(dir, "missing"), false) {
		t.Fatalf("stageDone must be false when marker missing")
	}
}
