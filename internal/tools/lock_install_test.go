package tools

import (
	"testing"

	"github.com/UberMorgott/morgue/internal/config"
)

func TestRecordInstallViaManager(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir, config.Config{})
	if err := m.RecordInstall("assetripper", "1.3.14"); err != nil {
		t.Fatalf("RecordInstall: %v", err)
	}
	lk, _ := ReadLock(dir)
	if lk.Tools["assetripper"] != "1.3.14" {
		t.Fatalf("lock not updated: %v", lk.Tools)
	}
}
