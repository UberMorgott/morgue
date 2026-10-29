package tools

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// ghidraJavaMinOf reads application.java.min from a Ghidra home
// (<home>/Ghidra/application.properties); 0 when absent or not an integer.
func ghidraJavaMinOf(home string) int {
	f, err := os.Open(filepath.Join(home, "Ghidra", "application.properties")) //nolint:gosec // G304: installed tool file
	if err != nil {
		return 0
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if ok && strings.TrimSpace(k) == "application.java.min" {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil {
				return 0
			}
			return n
		}
	}
	return 0
}

// javaRequirement is the JDK feature release the run's Java must satisfy:
// the installed (or pre-seeded) Ghidra's application.java.min, never below
// managedJavaFeature. The floor keeps a Ghidra auto-update during the run
// (whose newer release may need the current JDK) launchable; a Ghidra that
// declares a newer minimum raises the requirement and the managed install.
func (m *Manager) javaRequirement() int {
	req := managedJavaFeature
	if p, err := m.resolveUnbound("ghidra"); err == nil {
		if n := ghidraJavaMinOf(filepath.Dir(p)); n > req {
			req = n
		}
	}
	return req
}

// javaIsJDK reports whether bin has javac beside it (Ghidra's headless
// launcher and Native AOT recompiles reject a bare JRE).
func javaIsJDK(bin string) bool {
	javac := "javac"
	if runtime.GOOS == "windows" {
		javac = "javac.exe"
	}
	_, err := os.Stat(filepath.Join(filepath.Dir(bin), javac))
	return err == nil
}

// javaCandidates lists installed Java binaries in preference order: the
// managed portable JDK; JAVA_HOME and java on PATH only with the explicit
// prefer_system_tools opt-in.
func (m *Manager) javaCandidates() []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" {
			return
		}
		key := strings.ToLower(filepath.Clean(p))
		if seen[key] {
			return
		}
		if info, err := os.Stat(p); err != nil || info.IsDir() {
			return
		}
		seen[key] = true
		out = append(out, p)
	}
	add(m.localRuntimeBin(RuntimeJava))
	if !m.cfg.PreferSystemTools {
		return out
	}
	if home := strings.TrimSpace(os.Getenv("JAVA_HOME")); home != "" {
		add(filepath.Join(home, runtimeBinary(RuntimeJava)))
	}
	if p, err := exec.LookPath(runtimeSystemName(RuntimeJava)); err == nil {
		add(p)
	}
	return out
}

// selectJava returns the first installed JDK meeting the requirement, or ""
// when none does (the caller then installs the managed JDK).
func (m *Manager) selectJava(req int) string {
	for _, bin := range m.javaCandidates() {
		if javaMajor(javaVersionOf(bin)) >= req && javaIsJDK(bin) {
			return bin
		}
	}
	return ""
}

// javaSatisfies reports whether bin meets this manager's Java requirement.
func (m *Manager) javaSatisfies(bin string) bool {
	return javaMajor(javaVersionOf(bin)) >= m.javaRequirement() && javaIsJDK(bin)
}
