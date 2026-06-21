package recipe

import (
	"path/filepath"
	"testing"
)

func TestIL2CPPLayout(t *testing.T) {
	l := newIL2CPPLayout("D:/morgue-out", "LostCastle2")
	if l.CsDir != filepath.Join("D:/morgue-out", "LostCastle2", "dump", "cs") {
		t.Fatalf("CsDir = %q", l.CsDir)
	}
	if l.DllDir != filepath.Join("D:/morgue-out", "LostCastle2", "dump", "dll") {
		t.Fatalf("DllDir = %q", l.DllDir)
	}
	if l.DataDir != filepath.Join("D:/morgue-out", "LostCastle2", "data") {
		t.Fatalf("DataDir = %q", l.DataDir)
	}
	if l.TmpDir != filepath.Join("D:/morgue-out", "LostCastle2", ".tmp") {
		t.Fatalf("TmpDir = %q", l.TmpDir)
	}
}

// TestIL2CPPLayoutEmptyGame proves an empty game segment roots the layout
// directly at outRoot (the Execute path passes ctx.Output as the pre-namespaced
// root, so it must not introduce a second nesting level).
func TestIL2CPPLayoutEmptyGame(t *testing.T) {
	l := newIL2CPPLayout("D:/out/GameAssembly.dll", "")
	if l.Root != filepath.Clean("D:/out/GameAssembly.dll") {
		t.Fatalf("Root = %q, want the bare outRoot", l.Root)
	}
	if l.CsDir != filepath.Join("D:/out/GameAssembly.dll", "dump", "cs") {
		t.Fatalf("CsDir = %q", l.CsDir)
	}
}
