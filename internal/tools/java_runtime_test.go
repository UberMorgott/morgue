package tools

import (
	"os"
	"path/filepath"
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
	t.Setenv("PATH", t.TempDir())
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
