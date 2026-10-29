package util

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func envValue(env []string, key string) (string, bool) {
	val, found := "", false
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		if strings.EqualFold(k, key) {
			val, found = v, true
		}
	}
	return val, found
}

// Children never inherit a system runtime selector or a PATH dir holding a
// system java/dotnet/python unless prefer_system_tools is on.
func TestChildEnvStripsSystemRuntimes(t *testing.T) {
	exe := func(n string) string {
		if runtime.GOOS == "windows" {
			return n + ".exe"
		}
		return n
	}
	sysJava, sysDotnet, plain := t.TempDir(), t.TempDir(), t.TempDir()
	_ = os.WriteFile(filepath.Join(sysJava, exe("java")), []byte("x"), 0o755)
	_ = os.WriteFile(filepath.Join(sysDotnet, exe("dotnet")), []byte("x"), 0o755)
	_ = os.WriteFile(filepath.Join(plain, exe("tool")), []byte("x"), 0o755)
	t.Setenv("PATH", strings.Join([]string{sysJava, sysDotnet, plain}, string(os.PathListSeparator)))
	t.Setenv("JAVA_HOME", sysJava)
	t.Setenv("DOTNET_ROOT", sysDotnet)
	t.Setenv("PYTHONHOME", plain)
	t.Setenv("JAVA_TOOL_OPTIONS", "-Xmx64g")
	SetAllowSystemRuntimes(false)
	env := ChildEnv("JAVA_HOME=managed")
	for _, k := range []string{"DOTNET_ROOT", "PYTHONHOME", "JAVA_TOOL_OPTIONS"} {
		if v, ok := envValue(env, k); ok {
			t.Errorf("%s leaked: %s", k, v)
		}
	}
	if v, _ := envValue(env, "JAVA_HOME"); v != "managed" {
		t.Errorf("explicit managed JAVA_HOME lost: %q", v)
	}
	path, _ := envValue(env, "PATH")
	if strings.Contains(path, sysJava) || strings.Contains(path, sysDotnet) || !strings.Contains(path, plain) {
		t.Errorf("PATH not sanitized: %s", path)
	}
	if strings.Contains(ChildPath(), sysJava) {
		t.Errorf("ChildPath leaks system java dir")
	}

	SetAllowSystemRuntimes(true)
	t.Cleanup(func() { SetAllowSystemRuntimes(false) })
	env = ChildEnv()
	if v, _ := envValue(env, "DOTNET_ROOT"); v != sysDotnet {
		t.Errorf("prefer_system_tools must keep DOTNET_ROOT, got %q", v)
	}
	if path, _ := envValue(env, "PATH"); !strings.Contains(path, sysJava) {
		t.Errorf("prefer_system_tools must keep PATH")
	}
}
