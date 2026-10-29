package tools

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/UberMorgott/morgue/internal/config"
)

// stubJavaVersion makes every java binary report version v for this test.
func stubJavaVersion(t *testing.T, v string) {
	t.Helper()
	orig := javaVersionOf
	javaVersionOf = func(string) string { return v }
	t.Cleanup(func() { javaVersionOf = orig })
}

func TestJavaMajor(t *testing.T) {
	for in, want := range map[string]int{"21.0.10": 21, "25": 25, "25.0.1": 25, "1.8.0_402": 8, "": 0, "openjdk": 0} {
		if got := javaMajor(in); got != want {
			t.Errorf("javaMajor(%q) = %d, want %d", in, got, want)
		}
	}
}

// An older managed Java (the former JRE 21) is not selected: Ghidra 12.2
// would reject it after the run froze it.
func TestOldManagedJavaIsNotSelected(t *testing.T) {
	m := NewManager(filepath.Join(t.TempDir(), "tools"), config.Config{})
	bin := m.localRuntimeBin(RuntimeJava)
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("fake-java"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(bin), javacFile()), []byte("fake-javac"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	t.Setenv("JAVA_HOME", "")
	stubJavaVersion(t, "21.0.10")
	if p, err := m.runtimePathUnbound(RuntimeJava); err == nil {
		t.Fatalf("java 21 accepted: %s", p)
	} else if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
	stubJavaVersion(t, "25.0.1")
	if p, err := m.runtimePathUnbound(RuntimeJava); err != nil || p != bin {
		t.Fatalf("java 25 = %q, %v; want %s", p, err, bin)
	}
}

func javacFile() string {
	if runtime.GOOS == "windows" {
		return "javac.exe"
	}
	return "javac"
}

// fakeJDK lays out <home>/bin/java(+javac) and returns the java path.
func fakeJDK(t *testing.T, home string, jdk bool) string {
	t.Helper()
	bin := filepath.Join(home, runtimeBinary(RuntimeJava))
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("fake-java"), 0o755); err != nil {
		t.Fatal(err)
	}
	if jdk {
		if err := os.WriteFile(filepath.Join(filepath.Dir(bin), javacFile()), []byte("fake-javac"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return bin
}

func stubJavaVersions(t *testing.T, versions map[string]string) {
	t.Helper()
	orig := javaVersionOf
	javaVersionOf = func(bin string) string { return versions[filepath.Clean(bin)] }
	t.Cleanup(func() { javaVersionOf = orig })
}

// With the prefer_system_tools opt-in, selection picks the installed JDK that
// meets the Ghidra requirement: a JAVA_HOME JDK 25 wins over an older PATH
// java and over a JRE.
func TestJavaSelectionPicksMatchingInstalledJDK(t *testing.T) {
	m := NewManager(filepath.Join(t.TempDir(), "tools"), config.Config{PreferSystemTools: true})
	t.Cleanup(func() { NewManager(t.TempDir(), config.Config{}) })
	pathDir := t.TempDir()
	old := fakeJDK(t, pathDir, true)
	jre := fakeJDK(t, filepath.Join(t.TempDir(), "jre"), false)
	home := filepath.Join(t.TempDir(), "jdk25")
	good := fakeJDK(t, home, true)
	t.Setenv("PATH", filepath.Dir(old))
	t.Setenv("JAVA_HOME", home)
	t.Setenv("GHIDRA_HOME", "")
	stubJavaVersions(t, map[string]string{filepath.Clean(old): "21.0.4", filepath.Clean(jre): "25.0.1", filepath.Clean(good): "25.0.1"})
	if p, err := m.runtimePathUnbound(RuntimeJava); err != nil || p != good {
		t.Fatalf("selected %q, %v; want JAVA_HOME JDK %s", p, err, good)
	}
	t.Setenv("JAVA_HOME", filepath.Dir(filepath.Dir(jre)))
	if p, err := m.runtimePathUnbound(RuntimeJava); err == nil {
		t.Fatalf("JRE or older java accepted: %s", p)
	}
}

// A Ghidra declaring a newer application.java.min raises the requirement
// above the managed floor.
func TestJavaRequirementFollowsGhidraJavaMin(t *testing.T) {
	ghidraHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ghidraHome, "Ghidra"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ghidraHome, "Ghidra", "application.properties"), []byte("application.name=Ghidra\napplication.java.min=27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	launcher := "ghidraRun"
	if runtime.GOOS == "windows" {
		launcher = "ghidraRun.bat"
	}
	if err := os.WriteFile(filepath.Join(ghidraHome, launcher), []byte("@echo off\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := NewManager(filepath.Join(t.TempDir(), "tools"), config.Config{GhidraHome: ghidraHome})
	if got := m.javaRequirement(); got != 27 {
		t.Fatalf("requirement = %d, want 27 from application.java.min", got)
	}
	m = NewManager(filepath.Join(t.TempDir(), "tools"), config.Config{})
	if got := m.javaRequirement(); got != managedJavaFeature {
		t.Fatalf("requirement without Ghidra = %d, want %d", got, managedJavaFeature)
	}
}

// Default isolation: a system JDK/.NET first on PATH and in JAVA_HOME is never
// selected; runs resolve the managed runtimes under the Morgue folder, and a
// missing managed runtime is reported (selection then installs it).
func TestSystemRuntimesIgnoredByDefault(t *testing.T) {
	sys := t.TempDir()
	sysJava := fakeJDK(t, filepath.Join(sys, "jdk"), true)
	dotnetName := "dotnet"
	if runtime.GOOS == "windows" {
		dotnetName = "dotnet.exe"
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(sysJava), dotnetName), []byte("fake-dotnet"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(sysJava)+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("JAVA_HOME", filepath.Join(sys, "jdk"))
	stubJavaVersion(t, "25.0.1")
	base := filepath.Join(t.TempDir(), "tools")
	m := NewManager(base, config.Config{})
	for _, kind := range []RuntimeKind{RuntimeJava, RuntimeDotnet, RuntimeAspNet} {
		if p, err := m.runtimePathUnbound(kind); err == nil {
			t.Fatalf("%s resolved to system %s without prefer_system_tools", kind, p)
		}
	}
	managedJava := fakeJDK(t, m.localRuntimeDir(RuntimeJava), true)
	if err := os.MkdirAll(m.localRuntimeDir(RuntimeDotnet), 0o755); err != nil {
		t.Fatal(err)
	}
	managedDotnet := m.localRuntimeBin(RuntimeDotnet)
	if err := os.WriteFile(managedDotnet, []byte("fake-dotnet"), 0o755); err != nil {
		t.Fatal(err)
	}
	for kind, want := range map[RuntimeKind]string{RuntimeJava: managedJava, RuntimeDotnet: managedDotnet} {
		p, err := m.runtimePathUnbound(kind)
		if err != nil || p != want || !withinRoot(base, p) {
			t.Fatalf("%s = %q, %v; want managed %s under %s", kind, p, err, want, base)
		}
	}
	for _, st := range m.CheckRuntimes() {
		if st.Available && !st.Local {
			t.Fatalf("CheckRuntimes offers system %s at %s", st.Kind, st.Path)
		}
	}
}
