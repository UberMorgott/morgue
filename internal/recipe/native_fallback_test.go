package recipe

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/tools"
)

// testPEPath returns a path to a real Windows PE with an import table: the
// running test binary itself. This avoids hand-crafting a PE or shelling out to
// a compiler while still exercising the saferwall parser on a genuine PE.
func testPEPath(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("cannot resolve test executable: %v", err)
	}
	return exe
}

func requireNonEmpty(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", filepath.Base(path), err)
	}
	if info.Size() == 0 {
		t.Fatalf("expected %s to be non-empty", filepath.Base(path))
	}
}

func TestWriteImports(t *testing.T) {
	out := t.TempDir()
	n, err := writeImports(testPEPath(t), out)
	if err != nil {
		t.Fatalf("writeImports: %v", err)
	}
	if n == 0 {
		t.Fatal("writeImports reported 0 imported symbols for a real PE")
	}
	requireNonEmpty(t, filepath.Join(out, "imports.txt"))
	requireNonEmpty(t, filepath.Join(out, "imports.json"))

	data, _ := os.ReadFile(filepath.Join(out, "imports.txt"))
	if !strings.Contains(string(data), "!") {
		t.Errorf("imports.txt should contain DLL!Func lines, got: %.120q", string(data))
	}
}

func TestWriteStringsFallback(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "blob.bin")

	// ASCII string + a UTF-16LE ("wide") string in one buffer.
	var buf []byte
	buf = append(buf, []byte("AsciiMarkerString")...)
	buf = append(buf, 0x00)
	for _, r := range "WideMarkerString" {
		buf = append(buf, byte(r), 0x00)
	}
	if err := os.WriteFile(src, buf, 0644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "strings.txt")
	n, err := writeStringsFallback(src, out, 4)
	if err != nil {
		t.Fatalf("writeStringsFallback: %v", err)
	}
	if n == 0 {
		t.Fatal("writeStringsFallback reported 0 strings")
	}
	requireNonEmpty(t, out)

	got, _ := os.ReadFile(out)
	s := string(got)
	if !strings.Contains(s, "AsciiMarkerString") {
		t.Errorf("strings.txt missing ASCII string; got: %q", s)
	}
	if !strings.Contains(s, "WideMarkerString") {
		t.Errorf("strings.txt missing UTF-16LE string; got: %q", s)
	}
}

func TestWriteSectionSummary(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "sections.txt")
	n, err := writeSectionSummary(testPEPath(t), out)
	if err != nil {
		t.Fatalf("writeSectionSummary: %v", err)
	}
	if n == 0 {
		t.Fatal("writeSectionSummary reported 0 sections for a real PE")
	}
	requireNonEmpty(t, out)

	got, _ := os.ReadFile(out)
	if !strings.Contains(string(got), "entropy=") {
		t.Errorf("sections.txt should include entropy; got: %.200q", string(got))
	}
}

// TestNativeExecuteWithoutGhidra proves the native recipe produces non-zero
// output (imports + strings) even when neither the strings tool nor Ghidra is
// available — the Issue 2 unblock.
func TestNativeExecuteWithoutGhidra(t *testing.T) {
	outDir := t.TempDir()
	// Empty tools dir + no GHIDRA_HOME => neither strings nor ghidra resolvable.
	t.Setenv("GHIDRA_HOME", "")
	mgr := tools.NewManager(t.TempDir(), config.Config{})
	cfg := config.Config{NativeGhidraDecompile: true}

	ctx := &Context{
		Target: testPEPath(t),
		Output: outDir,
		Tools:  mgr,
		Ctx:    context.Background(),
		Config: &cfg,
	}

	n := &Native{}
	if err := n.Execute(ctx); err != nil {
		t.Fatalf("Native.Execute should not fail when Ghidra is absent, got: %v", err)
	}

	requireNonEmpty(t, filepath.Join(outDir, "imports.txt"))
	requireNonEmpty(t, filepath.Join(outDir, "imports.json"))
	requireNonEmpty(t, filepath.Join(outDir, "strings.txt"))
	requireNonEmpty(t, filepath.Join(outDir, "sections.txt"))
}
