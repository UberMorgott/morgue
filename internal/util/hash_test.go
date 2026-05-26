package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSHA256File(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.bin")
	if err := os.WriteFile(p, []byte("hello morgue"), 0644); err != nil {
		t.Fatal(err)
	}

	hash, err := SHA256File(p)
	if err != nil {
		t.Fatalf("SHA256File() error: %v", err)
	}

	if len(hash) != 64 {
		t.Errorf("SHA256File() hash length = %d, want 64", len(hash))
	}

	// Deterministic
	hash2, _ := SHA256File(p)
	if hash != hash2 {
		t.Error("SHA256File() not deterministic")
	}
}

func TestSHA256FileMissing(t *testing.T) {
	_, err := SHA256File("/nonexistent/file")
	if err == nil {
		t.Error("SHA256File() should error on missing file")
	}
}
