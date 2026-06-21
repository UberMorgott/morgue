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

func TestFileNonEmpty(t *testing.T) {
	dir := t.TempDir()

	// Missing path.
	if fileNonEmpty(filepath.Join(dir, "nope")) {
		t.Fatalf("fileNonEmpty(missing) = true")
	}
	// Empty dir.
	empty := filepath.Join(dir, "empty")
	os.MkdirAll(empty, 0755)
	if fileNonEmpty(empty) {
		t.Fatalf("fileNonEmpty(empty dir) = true")
	}
	// Dir with an entry.
	full := filepath.Join(dir, "full")
	os.MkdirAll(full, 0755)
	os.WriteFile(filepath.Join(full, "a.dll"), []byte("x"), 0644)
	if !fileNonEmpty(full) {
		t.Fatalf("fileNonEmpty(dir with file) = false")
	}
	// Empty file vs non-empty file.
	zero := filepath.Join(dir, "zero.txt")
	os.WriteFile(zero, nil, 0644)
	if fileNonEmpty(zero) {
		t.Fatalf("fileNonEmpty(empty file) = true")
	}
}

// TestDumpSkipGate proves the resume gate used in Execute: the dump is skipped
// only when the il2cpp.cs marker exists AND dump/dll holds assemblies, and only
// when force is false.
func TestDumpSkipGate(t *testing.T) {
	root := t.TempDir()
	l := newIL2CPPLayout(root, "")
	l.mkdirAll()
	csMarker := filepath.Join(l.CsDir, "il2cpp.cs")

	skip := func(force bool) bool { return stageDone(csMarker, force) && fileNonEmpty(l.DllDir) }

	// Nothing produced yet -> do not skip.
	if skip(false) {
		t.Fatalf("skip with no outputs")
	}
	// cs present but no DLLs -> do not skip.
	os.WriteFile(csMarker, []byte("// cs"), 0644)
	if skip(false) {
		t.Fatalf("skip with cs but empty dll dir")
	}
	// Both present -> skip.
	os.WriteFile(filepath.Join(l.DllDir, "Game.dll"), []byte("x"), 0644)
	if !skip(false) {
		t.Fatalf("should skip when cs + dll present")
	}
	// force always re-runs.
	if skip(true) {
		t.Fatalf("force must bypass skip")
	}
}
