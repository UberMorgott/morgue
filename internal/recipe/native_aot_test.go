package recipe

import (
	"archive/zip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestWriteRebuiltJarReplacesClassesKeepsResources(t *testing.T) {
	dir := t.TempDir()
	orig := filepath.Join(dir, "orig.jar")
	writeZip(t, orig, map[string]string{
		"META-INF/":            "",
		"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\n",
		"images/method.png":    "png",
		"nativeaot/Old.class":  "stale",
	})
	classes := filepath.Join(dir, "classes", "nativeaot")
	if err := os.MkdirAll(classes, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(classes, "New.class"), []byte("fresh"), 0644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.jar")
	if err := writeRebuiltJar(orig, filepath.Join(dir, "classes"), out); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	want := "META-INF/MANIFEST.MF,images/method.png,nativeaot/New.class"
	if got := strings.Join(names, ","); got != want {
		t.Fatalf("jar entries = %s, want %s", got, want)
	}
}

func TestUnzipJavaRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.zip")
	writeZip(t, good, map[string]string{"src/main/java/a/A.java": "class A {}", "README.txt": "x"})
	srcs, err := unzipJava(good, filepath.Join(dir, "out"))
	if err != nil || len(srcs) != 1 {
		t.Fatalf("unzipJava = %v, %v; want 1 source", srcs, err)
	}
	bad := filepath.Join(dir, "bad.zip")
	writeZip(t, bad, map[string]string{"../evil.java": "x"})
	if _, err := unzipJava(bad, filepath.Join(dir, "out2")); err == nil {
		t.Fatal("unzipJava accepted a path-traversal entry")
	}
}

func TestReadProperty(t *testing.T) {
	p := filepath.Join(t.TempDir(), "extension.properties")
	if err := os.WriteFile(p, []byte("name=ghidra-nativeaot\nversion=12.0.1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if v := readProperty(p, "version"); v != "12.0.1" {
		t.Fatalf("readProperty = %q", v)
	}
	if v := readProperty(p, "missing"); v != "" {
		t.Fatalf("readProperty(missing) = %q", v)
	}
}

func TestNativeAOTPreScriptNamesAnalyzer(t *testing.T) {
	if !strings.Contains(nativeAOTPreScript.Source, `"Native AOT Analyzer"`) ||
		!strings.Contains(nativeAOTPreScript.Source, "class MorgueNativeAot ") {
		t.Fatal("pre-script must enable the analyzer and match its file name")
	}
}
