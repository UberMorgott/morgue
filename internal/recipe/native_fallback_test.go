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

	data, _ := os.ReadFile(filepath.Join(out, "imports.txt")) //nolint:gosec // G304: test fixture path built from t.TempDir(), not user input
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
		buf = append(buf, byte(r), 0x00) //nolint:gosec // G115: test fixture values are small constants that cannot overflow
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

	got, _ := os.ReadFile(out) //nolint:gosec // G304: test fixture path built from t.TempDir(), not user input
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

	got, _ := os.ReadFile(out) //nolint:gosec // G304: test fixture path built from t.TempDir(), not user input
	if !strings.Contains(string(got), "entropy=") {
		t.Errorf("sections.txt should include entropy; got: %.200q", string(got))
	}
}

func TestWritePEExtras(t *testing.T) {
	out := t.TempDir()
	_, _, err := writePEExtras(testPEPath(t), out)
	if err != nil {
		t.Fatalf("writePEExtras: %v", err)
	}
	// Every artifact must exist and carry either data or an explanatory note.
	for _, name := range []string{"exports.txt", "resources.txt", "tls.txt", "debug.txt"} {
		requireNonEmpty(t, filepath.Join(out, name))
	}
	// The Go test binary is not stripped, so it always has a TLS directory.
	tls, _ := os.ReadFile(filepath.Join(out, "tls.txt")) //nolint:gosec // G304: test fixture path built from t.TempDir(), not user input
	if !strings.Contains(string(tls), "callbacks=") && !strings.Contains(string(tls), "#") {
		t.Errorf("tls.txt should report callbacks or a note; got: %.120q", string(tls))
	}
}

// TestWritePEExtrasNonPE proves a non-PE input neither panics nor errors: the
// artifacts are still written, each carrying a skip note.
func TestWritePEExtrasNonPE(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "not-a-pe.bin")
	if err := os.WriteFile(src, []byte("definitely not a PE file at all"), 0644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")

	n, notes, err := writePEExtras(src, out)
	if err != nil {
		t.Fatalf("writePEExtras on non-PE should not error, got: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 exports for a non-PE, got %d", n)
	}
	if len(notes) == 0 {
		t.Error("expected a skip note for a non-PE input")
	}
	for _, name := range []string{"exports.txt", "resources.txt", "tls.txt", "debug.txt"} {
		got, rerr := os.ReadFile(filepath.Join(out, name)) //nolint:gosec // G304: test fixture path built from t.TempDir(), not user input
		if rerr != nil {
			t.Fatalf("expected %s to exist: %v", name, rerr)
		}
		if !strings.HasPrefix(string(got), "#") {
			t.Errorf("%s should start with a skip note; got: %.80q", name, string(got))
		}
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
	// Ghidra absent => the native fallback artifacts must still be written.
	requireNonEmpty(t, filepath.Join(outDir, "exports.txt"))
	requireNonEmpty(t, filepath.Join(outDir, "resources.txt"))
	requireNonEmpty(t, filepath.Join(outDir, "tls.txt"))
	requireNonEmpty(t, filepath.Join(outDir, "debug.txt"))
}
