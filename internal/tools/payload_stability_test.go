package tools

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Extraction keeps archive mtimes so tools that compare them (Ghidra .sla vs
// .slaspec) do not rebuild, i.e. mutate, their payload after selection.
func TestExtractZipPreservesModTime(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.zip")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	stamp := time.Date(2025, 3, 4, 5, 6, 8, 0, time.UTC)
	w, err := zw.CreateHeader(&zip.FileHeader{Name: "x86.sla", Method: zip.Deflate, Modified: stamp})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("sla"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	out := filepath.Join(dir, "out")
	if err := extractZip(archive, out); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(out, "x86.sla"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(stamp) {
		t.Fatalf("mtime = %v, want %v", info.ModTime().UTC(), stamp)
	}
}

func TestInventoryIgnoresGhidraSlaLocks(t *testing.T) {
	root := t.TempDir()
	lang := filepath.Join(root, "Ghidra", "Processors", "x86", "data", "languages")
	if err := os.MkdirAll(lang, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lang, "x86.sla"), []byte("sla"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := inventoryHash(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lang, "x86.sla.lock"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := inventoryHash(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("Ghidra .sla.lock sentinel changed the payload hash")
	}
}
