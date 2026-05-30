package recipe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildIndexHappyPath verifies buildIndex catalogs only source files,
// sums their sizes, counts strings.txt lines, and writes a valid index.json
// that round-trips to the returned struct.
func TestBuildIndexHappyPath(t *testing.T) {
	dir := t.TempDir()

	// Source files with known byte contents. countLines counts '\n' bytes:
	// use newline-terminated lines so counts are deterministic.
	aContent := []byte("int a(void) { return 1; }\n")        // 26 bytes, 1 newline
	bContent := []byte("class B {\nvoid f() {}\n};\n")        // 25 bytes, 3 newlines
	if err := os.WriteFile(filepath.Join(dir, "a.c"), aContent, 0644); err != nil {
		t.Fatal(err)
	}
	subDir := filepath.Join(dir, "sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "b.cpp"), bContent, 0644); err != nil {
		t.Fatal(err)
	}

	// Non-source file that must NOT be counted.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore me"), 0644); err != nil {
		t.Fatal(err)
	}

	// strings.txt at root: countLines counts '\n' bytes, so N trailing
	// newlines => N. Use 3 lines each newline-terminated.
	const stringLines = 3
	stringsContent := strings.Repeat("some string\n", stringLines)
	if err := os.WriteFile(filepath.Join(dir, "strings.txt"), []byte(stringsContent), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := buildIndex(dir)
	if err != nil {
		t.Fatalf("buildIndex returned error: %v", err)
	}

	wantBytes := int64(len(aContent) + len(bContent))

	if idx.FileCount != 2 {
		t.Errorf("FileCount = %d, want 2 (only source files, notes.txt and strings.txt excluded)", idx.FileCount)
	}
	if idx.TotalBytes != wantBytes {
		t.Errorf("TotalBytes = %d, want %d (sum of source file sizes)", idx.TotalBytes, wantBytes)
	}
	if idx.StringsLine != stringLines {
		t.Errorf("StringsLine = %d, want %d", idx.StringsLine, stringLines)
	}

	// Per-file line counts (newline bytes): a.c=1, b.cpp=3, total=4.
	const wantTotalLines = 4
	if idx.TotalLines != wantTotalLines {
		t.Errorf("TotalLines = %d, want %d", idx.TotalLines, wantTotalLines)
	}
	wantLines := map[string]int{"a.c": 1, "sub/b.cpp": 3}
	gotLines := 0
	for _, e := range idx.Files {
		if want, ok := wantLines[e.Path]; ok {
			if e.Lines != want {
				t.Errorf("Files entry %q Lines = %d, want %d", e.Path, e.Lines, want)
			}
		} else {
			t.Errorf("unexpected source entry %q", e.Path)
		}
		gotLines += e.Lines
	}
	if gotLines != idx.TotalLines {
		t.Errorf("sum of entry Lines = %d, want TotalLines %d", gotLines, idx.TotalLines)
	}

	// index.json must have been written to the indexed dir.
	indexPath := filepath.Join(dir, "index.json")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("index.json not written: %v", err)
	}

	// It must be valid JSON matching the returned struct.
	var fromDisk outputIndex
	if err := json.Unmarshal(data, &fromDisk); err != nil {
		t.Fatalf("index.json is not valid JSON: %v", err)
	}
	if fromDisk.FileCount != idx.FileCount {
		t.Errorf("disk FileCount = %d, want %d", fromDisk.FileCount, idx.FileCount)
	}
	if fromDisk.TotalBytes != idx.TotalBytes {
		t.Errorf("disk TotalBytes = %d, want %d", fromDisk.TotalBytes, idx.TotalBytes)
	}
	if fromDisk.StringsLine != idx.StringsLine {
		t.Errorf("disk StringsLine = %d, want %d", fromDisk.StringsLine, idx.StringsLine)
	}
	if fromDisk.GeneratedAt != idx.GeneratedAt {
		t.Errorf("disk GeneratedAt = %q, want %q", fromDisk.GeneratedAt, idx.GeneratedAt)
	}
	if len(fromDisk.Files) != len(idx.Files) {
		t.Fatalf("disk Files len = %d, want %d", len(fromDisk.Files), len(idx.Files))
	}

	// Verify per-entry contents match between memory and disk.
	for i := range idx.Files {
		if fromDisk.Files[i] != idx.Files[i] {
			t.Errorf("Files[%d] disk=%+v memory=%+v", i, fromDisk.Files[i], idx.Files[i])
		}
	}
}

// TestBuildIndexParentStringsFallback verifies that when strings.txt is absent
// from the indexed dir but present in its parent (= ctx.Output, where recipes
// actually write it next to src/), its line count is still recorded.
func TestBuildIndexParentStringsFallback(t *testing.T) {
	parent := t.TempDir()
	srcDir := filepath.Join(parent, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	// A source file in srcDir so the index is non-trivial.
	if err := os.WriteFile(filepath.Join(srcDir, "a.c"), []byte("int a(void){return 1;}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// strings.txt only in the PARENT, not in srcDir.
	const stringLines = 5
	stringsContent := strings.Repeat("some string\n", stringLines)
	if err := os.WriteFile(filepath.Join(parent, "strings.txt"), []byte(stringsContent), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(srcDir, "strings.txt")); !os.IsNotExist(err) {
		t.Fatalf("precondition: srcDir/strings.txt must not exist, got err=%v", err)
	}

	idx, err := buildIndex(srcDir)
	if err != nil {
		t.Fatalf("buildIndex returned error: %v", err)
	}
	if idx.StringsLine != stringLines {
		t.Errorf("StringsLine = %d, want %d (parent-dir fallback)", idx.StringsLine, stringLines)
	}
}

// TestBuildIndexForwardSlashes verifies nested-entry relative paths use forward
// slashes (filepath.ToSlash), never backslashes — critical on Windows.
func TestBuildIndexForwardSlashes(t *testing.T) {
	dir := t.TempDir()

	nested := filepath.Join(dir, "sub", "deeper")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "b.cpp"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := buildIndex(dir)
	if err != nil {
		t.Fatalf("buildIndex returned error: %v", err)
	}

	var found bool
	for _, e := range idx.Files {
		if strings.Contains(e.Path, "\\") {
			t.Errorf("entry path %q contains a backslash; must use forward slashes", e.Path)
		}
		if e.Path == "sub/deeper/b.cpp" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected entry %q not found in %+v", "sub/deeper/b.cpp", idx.Files)
	}
}

// TestBuildIndexEmptyDir verifies an empty directory yields an empty,
// error-free index that is still written to disk.
func TestBuildIndexEmptyDir(t *testing.T) {
	dir := t.TempDir()

	idx, err := buildIndex(dir)
	if err != nil {
		t.Fatalf("buildIndex on empty dir returned error: %v", err)
	}
	if idx.FileCount != 0 {
		t.Errorf("FileCount = %d, want 0", idx.FileCount)
	}
	if idx.TotalBytes != 0 {
		t.Errorf("TotalBytes = %d, want 0", idx.TotalBytes)
	}
	if idx.StringsLine != 0 {
		t.Errorf("StringsLine = %d, want 0", idx.StringsLine)
	}
	if len(idx.Files) != 0 {
		t.Errorf("Files = %+v, want empty", idx.Files)
	}
	// index.json is still written even for an empty dir.
	if _, err := os.Stat(filepath.Join(dir, "index.json")); err != nil {
		t.Errorf("index.json not written for empty dir: %v", err)
	}
}

// TestBuildUEIndexCatalogsAssets verifies the UE index catalogs BOTH decompiled
// source under src/ and extracted game assets under extracted/, writes
// index.json to the outDir root (not inside src/), records 0 lines for binary
// assets, keeps line counts for source, and uses forward-slash relative paths.
func TestBuildUEIndexCatalogsAssets(t *testing.T) {
	out := t.TempDir()
	srcDir := filepath.Join(out, "src")
	extractedDir := filepath.Join(out, "extracted")
	if err := os.MkdirAll(filepath.Join(srcDir, "deep"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(extractedDir, "Game", "Content"), 0755); err != nil {
		t.Fatal(err)
	}

	srcContent := []byte("void f(){}\n") // 1 newline
	if err := os.WriteFile(filepath.Join(srcDir, "deep", "a.c"), srcContent, 0644); err != nil {
		t.Fatal(err)
	}
	assetContent := []byte("BINARYDATA")
	if err := os.WriteFile(filepath.Join(extractedDir, "Game", "Content", "Mesh.uasset"), assetContent, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extractedDir, "Game", "Content", "Mesh.uexp"), assetContent, 0644); err != nil {
		t.Fatal(err)
	}
	// A non-cataloged file that must be ignored.
	if err := os.WriteFile(filepath.Join(extractedDir, "readme.md"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := buildUEIndex(out, srcDir, extractedDir)
	if err != nil {
		t.Fatalf("buildUEIndex error: %v", err)
	}

	if idx.FileCount != 3 {
		t.Errorf("FileCount = %d, want 3 (a.c + 2 assets, readme.md ignored)", idx.FileCount)
	}
	wantBytes := int64(len(srcContent) + 2*len(assetContent))
	if idx.TotalBytes != wantBytes {
		t.Errorf("TotalBytes = %d, want %d", idx.TotalBytes, wantBytes)
	}
	// Only the source file contributes lines; assets record 0.
	if idx.TotalLines != 1 {
		t.Errorf("TotalLines = %d, want 1 (only a.c)", idx.TotalLines)
	}

	byPath := map[string]indexEntry{}
	for _, e := range idx.Files {
		if strings.Contains(e.Path, "\\") {
			t.Errorf("entry path %q contains a backslash", e.Path)
		}
		byPath[e.Path] = e
	}
	if e, ok := byPath["src/deep/a.c"]; !ok {
		t.Errorf("missing src entry; got %+v", idx.Files)
	} else if e.Lines != 1 {
		t.Errorf("src a.c Lines = %d, want 1", e.Lines)
	}
	if e, ok := byPath["extracted/Game/Content/Mesh.uasset"]; !ok {
		t.Errorf("missing .uasset entry; got %+v", idx.Files)
	} else if e.Lines != 0 {
		t.Errorf(".uasset Lines = %d, want 0 (binary asset)", e.Lines)
	}
	if _, ok := byPath["extracted/Game/Content/Mesh.uexp"]; !ok {
		t.Errorf("missing .uexp entry; got %+v", idx.Files)
	}

	// index.json must be written at outDir root, not inside src/.
	if _, statErr := os.Stat(filepath.Join(out, "index.json")); statErr != nil {
		t.Errorf("index.json not written to outDir root: %v", statErr)
	}
}

// TestBuildUEIndexExtractedOnly verifies a paks-only UE target (no src/) still
// produces a non-empty index from extracted assets alone.
func TestBuildUEIndexExtractedOnly(t *testing.T) {
	out := t.TempDir()
	extractedDir := filepath.Join(out, "extracted")
	if err := os.MkdirAll(extractedDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extractedDir, "Level.umap"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := buildUEIndex(out, "", extractedDir)
	if err != nil {
		t.Fatalf("buildUEIndex error: %v", err)
	}
	if idx.FileCount != 1 {
		t.Errorf("FileCount = %d, want 1 (.umap)", idx.FileCount)
	}
}

// TestBuildIndexNonExistentDir documents buildIndex's real contract for a
// non-existent srcDir: WalkDir's error short-circuits the walk callback (no
// panic), so buildIndex returns an empty index with no error and still writes
// index.json into the (now-created) directory path's parent — here we only
// assert it does not error and returns an empty index, matching actual behavior.
func TestBuildIndexNonExistentDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")

	idx, err := buildIndex(dir)
	// Real behavior: WalkDir reports the missing root via the err arg, the
	// callback returns nil, so the walk yields nothing. The only way buildIndex
	// errors here is if os.WriteFile of index.json fails (parent missing).
	if err == nil {
		// If no error, the index must be empty and index.json must exist.
		if idx == nil {
			t.Fatal("buildIndex returned nil index and nil error")
		}
		if idx.FileCount != 0 || idx.TotalBytes != 0 || len(idx.Files) != 0 {
			t.Errorf("non-existent dir produced non-empty index: %+v", idx)
		}
		if _, statErr := os.Stat(filepath.Join(dir, "index.json")); statErr != nil {
			t.Errorf("expected index.json written, got: %v", statErr)
		}
	} else {
		// If it errors (WriteFile into a missing dir), the index is nil — that
		// is also an acceptable, graceful contract.
		if idx != nil {
			t.Errorf("error returned but index non-nil: %+v", idx)
		}
	}
}
