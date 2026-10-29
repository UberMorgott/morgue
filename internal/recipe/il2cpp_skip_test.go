package recipe

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/tools"
)

func TestIL2CPPIgnoresPriorDumpMarkers(t *testing.T) {
	game := t.TempDir()
	target := filepath.Join(game, "GameAssembly.dll")
	if err := os.WriteFile(target, []byte("new input"), 0600); err != nil {
		t.Fatal(err)
	}
	metadata := make([]byte, 8)
	binary.LittleEndian.PutUint32(metadata, 0xFAB11BAF)
	binary.LittleEndian.PutUint32(metadata[4:], 29)
	if err := os.WriteFile(filepath.Join(game, "global-metadata.dat"), metadata, 0600); err != nil {
		t.Fatal(err)
	}
	l := newIL2CPPLayout(t.TempDir(), "")
	if err := l.mkdirAll(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(l.CsDir, "il2cpp.cs"), []byte("stale dump"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(l.DllDir, "Game.dll"), []byte("stale assembly"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	ctx := &Context{Ctx: context.Background(), Target: target, Output: l.Root, Config: &cfg, Tools: tools.NewManager(t.TempDir(), cfg)}
	err := (&IL2CPP{}).Execute(ctx)
	if err == nil || !strings.Contains(err.Error(), "no IL2CPP dumper succeeded") {
		t.Fatalf("prior marker bypassed metadata extraction: %v", err)
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
	_ = os.MkdirAll(empty, 0755)
	if fileNonEmpty(empty) {
		t.Fatalf("fileNonEmpty(empty dir) = true")
	}
	// Dir with an entry.
	full := filepath.Join(dir, "full")
	_ = os.MkdirAll(full, 0755)
	_ = os.WriteFile(filepath.Join(full, "a.dll"), []byte("x"), 0644)
	if !fileNonEmpty(full) {
		t.Fatalf("fileNonEmpty(dir with file) = false")
	}
	// Empty file vs non-empty file.
	zero := filepath.Join(dir, "zero.txt")
	_ = os.WriteFile(zero, nil, 0644)
	if fileNonEmpty(zero) {
		t.Fatalf("fileNonEmpty(empty file) = true")
	}
}
