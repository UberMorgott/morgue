package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/UberMorgott/morgue/internal/config"
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

// TestConcurrentRecordInstallPreservesEntries: concurrent installs of different
// tools must each end up in the lock; the mutex-guarded read-modify-write must
// not let one writer clobber another's entry.
func TestConcurrentRecordInstallPreservesEntries(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir, config.Config{})
	const n = 25
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := m.RecordInstall(fmt.Sprintf("tool-%02d", i), fmt.Sprintf("v%d", i)); err != nil {
				t.Errorf("RecordInstall %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	lk, err := ReadLock(dir)
	if err != nil {
		t.Fatalf("ReadLock: %v", err)
	}
	if len(lk.Tools) != n {
		t.Fatalf("lock has %d entries, want %d (entries clobbered): %v", len(lk.Tools), n, lk.Tools)
	}
	for i := range n {
		key := fmt.Sprintf("tool-%02d", i)
		if got := lk.Tools[key]; got != fmt.Sprintf("v%d", i) {
			t.Fatalf("entry %s = %q, want v%d", key, got, i)
		}
	}
}

// TestReadLockCorruptSurfacesError: a corrupt tools.lock.json must return an error
// (distinguishable from "missing") and an empty, non-nil map — not crash.
func TestReadLockCorruptSurfacesError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, lockFile), []byte("{ this is not json"), 0644); err != nil {
		t.Fatal(err)
	}
	lk, err := ReadLock(dir)
	if err == nil {
		t.Fatal("corrupt lock must surface a parse error, not nil")
	}
	if lk.Tools == nil {
		t.Fatal("corrupt lock must still yield a non-nil (empty) map")
	}
	if len(lk.Tools) != 0 {
		t.Fatalf("corrupt lock should yield empty map, got %v", lk.Tools)
	}
	// RecordInstall must still succeed over a corrupt lock (recoverable).
	m := NewManager(dir, config.Config{})
	if err := m.RecordInstall("recover", "1.0"); err != nil {
		t.Fatalf("RecordInstall over corrupt lock should recover: %v", err)
	}
	lk2, err := ReadLock(dir)
	if err != nil {
		t.Fatalf("ReadLock after recovery: %v", err)
	}
	if lk2.Tools["recover"] != "1.0" {
		t.Fatalf("recovery entry missing: %v", lk2.Tools)
	}
}
