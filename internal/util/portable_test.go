package util

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func envMap(env []string) map[string]string {
	m := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		m[strings.ToUpper(k)] = v // last wins, like exec.Cmd
	}
	return m
}

func TestPortableEnvStaysInDataDir(t *testing.T) {
	data := filepath.Join(t.TempDir(), "data")
	tmp := filepath.Join(data, "tmp", "p1")
	env := envMap(PortableEnv(data, tmp))

	for _, k := range []string{"TEMP", "TMP", "TMPDIR"} {
		if env[k] != tmp {
			t.Errorf("%s = %q, want %q", k, env[k], tmp)
		}
	}
	pathKeys := []string{
		"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME",
		"DOTNET_CLI_HOME", "DOTNET_BUNDLE_EXTRACT_BASE_DIR",
		"NUGET_PACKAGES", "NUGET_HTTP_CACHE_PATH", "NUGET_PLUGINS_CACHE_PATH",
	}
	for _, k := range pathKeys {
		v, ok := env[k]
		if !ok {
			t.Errorf("%s missing", k)
			continue
		}
		if !strings.HasPrefix(v, data+string(os.PathSeparator)) {
			t.Errorf("%s = %q escapes data dir %q", k, v, data)
		}
	}
	for k, want := range map[string]string{
		"DOTNET_CLI_TELEMETRY_OPTOUT":       "1",
		"DOTNET_NOLOGO":                     "1",
		"DOTNET_SKIP_FIRST_TIME_EXPERIENCE": "1",
	} {
		if env[k] != want {
			t.Errorf("%s = %q, want %q", k, env[k], want)
		}
	}
}

func TestChildEnvLayering(t *testing.T) {
	t.Setenv("MORGUE_TEST_INHERITED", "yes")
	env := ChildEnv("TEMP=override", "MORGUE_TEST_EXTRA=1")
	m := envMap(env)
	if m["MORGUE_TEST_INHERITED"] != "yes" {
		t.Error("parent environment not inherited")
	}
	if m["TEMP"] != "override" {
		t.Errorf("caller extra must win, TEMP = %q", m["TEMP"])
	}
	if m["MORGUE_TEST_EXTRA"] != "1" {
		t.Error("extra var missing")
	}
	if m["XDG_CONFIG_HOME"] != filepath.Join(DataDir(), "config") {
		t.Errorf("XDG_CONFIG_HOME = %q, want under DataDir()", m["XDG_CONFIG_HOME"])
	}
}

func TestInitPortableAt(t *testing.T) {
	for _, k := range []string{"TEMP", "TMP", "TMPDIR"} {
		t.Setenv(k, os.Getenv(k)) // restored after the test
	}
	t.Cleanup(func() {
		procTempMu.Lock()
		procTemp = ""
		procTempMu.Unlock()
	})

	data := filepath.Join(t.TempDir(), "data")
	tmpRoot := filepath.Join(data, "tmp")
	// A dir of a dead PID must be swept; one of a live PID (our parent) kept.
	dead := filepath.Join(tmpRoot, "p999999999")
	live := filepath.Join(tmpRoot, "p"+strconv.Itoa(os.Getppid()))
	other := filepath.Join(tmpRoot, "keep-me")
	for _, d := range []string{dead, live, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	cleanup, err := initPortableAt(data)
	if err != nil {
		t.Fatalf("initPortableAt: %v", err)
	}
	want := filepath.Join(tmpRoot, "p"+strconv.Itoa(os.Getpid()))
	if got := os.Getenv("TEMP"); got != want {
		t.Errorf("TEMP = %q, want %q", got, want)
	}
	if got := os.TempDir(); got != want {
		t.Errorf("os.TempDir() = %q, want %q", got, want)
	}
	if ProcTempDir() != want {
		t.Errorf("ProcTempDir() = %q, want %q", ProcTempDir(), want)
	}
	if _, err := os.Stat(dead); !os.IsNotExist(err) {
		t.Errorf("stale dir of dead pid not swept: %v", err)
	}
	for _, d := range []string{live, other} {
		if _, err := os.Stat(d); err != nil {
			t.Errorf("%s must be kept: %v", d, err)
		}
	}

	cleanup()
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Errorf("cleanup left %s: %v", want, err)
	}
}

func TestInitPortableNotWritable(t *testing.T) {
	// A regular file where the data dir should be: MkdirAll must fail and the
	// error must say why instead of falling back to a system dir.
	blocker := filepath.Join(t.TempDir(), "data")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := initPortableAt(blocker)
	if err == nil {
		t.Fatal("want error for unwritable data dir")
	}
	if !strings.Contains(err.Error(), "portable") || !strings.Contains(err.Error(), blocker) {
		t.Errorf("unhelpful error: %v", err)
	}
}

func TestDataPaths(t *testing.T) {
	base := BaseDir()
	if DataDir() != filepath.Join(base, "data") {
		t.Errorf("DataDir = %q", DataDir())
	}
	if APITokenPath() != filepath.Join(base, "data", "api-token") {
		t.Errorf("APITokenPath = %q", APITokenPath())
	}
	if TempRoot() != filepath.Join(base, "data", "tmp") {
		t.Errorf("TempRoot = %q", TempRoot())
	}
}
