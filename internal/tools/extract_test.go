package tools

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

// TestExtractZipRejectsSlip: an entry whose name escapes the destination must
// never be written outside destDir.
func TestExtractZipRejectsSlip(t *testing.T) {
	root := t.TempDir()
	destDir := filepath.Join(root, "dest")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatal(err)
	}

	archivePath := filepath.Join(root, "evil.zip")
	f, err := os.Create(filepath.Clean(archivePath))
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, name := range []string{"../escaped.txt", "..\\escaped2.txt", "ok.txt"} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	if err := extractZip(archivePath, destDir); err != nil {
		t.Fatalf("extractZip: %v", err)
	}

	for _, escaped := range []string{"escaped.txt", "escaped2.txt"} {
		if _, err := os.Stat(filepath.Join(root, escaped)); err == nil {
			t.Fatalf("zip slip: %s written outside destDir", escaped)
		}
	}
	if _, err := os.Stat(filepath.Join(destDir, "ok.txt")); err != nil {
		t.Fatalf("benign entry not extracted: %v", err)
	}
}
