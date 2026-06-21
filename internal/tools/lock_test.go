package tools

import (
	"path/filepath"
	"testing"
)

func TestLockRoundTrip(t *testing.T) {
	dir := t.TempDir()
	lk := Lock{Tools: map[string]string{"il2cppinspector": "2026.2"}}
	if err := WriteLock(dir, lk); err != nil {
		t.Fatalf("WriteLock: %v", err)
	}
	got, err := ReadLock(dir)
	if err != nil {
		t.Fatalf("ReadLock: %v", err)
	}
	if got.Tools["il2cppinspector"] != "2026.2" {
		t.Fatalf("lock value = %q", got.Tools["il2cppinspector"])
	}
}

func TestReadLockMissingIsEmpty(t *testing.T) {
	got, err := ReadLock(t.TempDir())
	if err != nil {
		t.Fatalf("ReadLock missing: %v", err)
	}
	if len(got.Tools) != 0 {
		t.Fatalf("expected empty lock, got %v", got.Tools)
	}
	_ = filepath.Separator
}
