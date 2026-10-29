package recipe

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseJavaMajor(t *testing.T) {
	cases := map[string]int{
		`openjdk version "25.0.1" 2025-10-21 LTS`: 25,
		`java version "1.8.0_431"`:                8,
		`openjdk version "21" 2023-09-19`:         21,
		`garbage`:                                 0,
	}
	for in, want := range cases {
		if got := parseJavaMajor(in); got != want {
			t.Errorf("parseJavaMajor(%q) = %d, want %d", in, got, want)
		}
	}
}

// fakeGhidraJava lays out a Ghidra home declaring application.java.min=minVer
// and a java[.exe] (optionally with javac) whose `-version` banner is banner.
func fakeGhidraJava(t *testing.T, minVer, banner string, withJavac bool) (ghidraHome, javaPath string) {
	t.Helper()
	root := t.TempDir()
	ghidraHome = filepath.Join(root, "ghidra")
	if err := os.MkdirAll(filepath.Join(ghidraHome, "Ghidra"), 0755); err != nil {
		t.Fatal(err)
	}
	props := "application.name=Ghidra\napplication.version=12.2\n"
	if minVer != "" {
		props += "application.java.min=" + minVer + "\n"
	}
	if err := os.WriteFile(filepath.Join(ghidraHome, "Ghidra", "application.properties"), []byte(props), 0644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "jdk", "bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	javaPath = filepath.Join(bin, "java.exe")
	_ = os.WriteFile(javaPath, []byte("x"), 0755)
	if withJavac {
		_ = os.WriteFile(filepath.Join(bin, javacName()), []byte("x"), 0755)
	}
	orig := javaVersionOutput
	javaVersionOutput = func(context.Context, string) (string, error) { return banner, nil }
	t.Cleanup(func() { javaVersionOutput = orig })
	return ghidraHome, javaPath
}

func TestCheckGhidraJava(t *testing.T) {
	ctx := context.Background()

	home, java := fakeGhidraJava(t, "25", `openjdk version "25.0.1"`, true)
	if err := checkGhidraJava(ctx, home, java); err != nil {
		t.Fatalf("JDK 25 vs min 25: unexpected error %v", err)
	}

	home, java = fakeGhidraJava(t, "25", `openjdk version "21.0.5"`, true)
	err := checkGhidraJava(ctx, home, java)
	if err == nil || !strings.Contains(err.Error(), "JDK 25+") || !strings.Contains(err.Error(), "version 21") {
		t.Fatalf("JDK 21 vs min 25: want clear version error, got %v", err)
	}

	home, java = fakeGhidraJava(t, "25", `openjdk version "25.0.1"`, false)
	if err := checkGhidraJava(ctx, home, java); err == nil || !strings.Contains(err.Error(), "JRE without javac") {
		t.Fatalf("JRE without javac: want JDK error, got %v", err)
	}

	home, _ = fakeGhidraJava(t, "25", "", true)
	if err := checkGhidraJava(ctx, home, ""); err == nil || !strings.Contains(err.Error(), "no Java runtime") {
		t.Fatalf("no java: want missing-runtime error, got %v", err)
	}

	home, java = fakeGhidraJava(t, "", `garbage`, false)
	if err := checkGhidraJava(ctx, home, java); err != nil {
		t.Fatalf("unknown requirement must defer to Ghidra, got %v", err)
	}
}
