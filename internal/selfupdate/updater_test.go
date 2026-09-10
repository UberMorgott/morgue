package selfupdate

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"0.7.3", "0.7.2", true},
		{"v0.7.3", "v0.7.2", true},
		{"0.7.2", "0.7.2", false},
		{"0.7.2", "0.7.3", false},
		{"0.7.2", "v0.7.2-34-gbaae1cb-dirty", false}, // dev build of the same release
		{"0.7.3", "v0.7.2-34-gbaae1cb", true},
		{"0.7.2", "dev", true},       // unparseable current → always offer
		{"nonsense", "0.7.2", false}, // unparseable latest → never offer
	}
	for _, c := range cases {
		if got := isNewer(c.latest, c.current); got != c.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}

func TestSelectAsset(t *testing.T) {
	assets := []releaseAsset{
		{Name: "checksums.txt"},
		{Name: "morgue_windows_arm64.zip", Size: 1},
		{Name: "morgue_windows_amd64.zip.sha256"},
		{Name: "morgue_windows_amd64.zip", Size: 2},
		{Name: "morgue_linux_amd64.zip", Size: 3},
	}
	cases := []struct {
		goos, goarch string
		want         string
	}{
		{"windows", "amd64", "morgue_windows_amd64.zip"},
		{"windows", "arm64", "morgue_windows_arm64.zip"},
		{"linux", "amd64", "morgue_linux_amd64.zip"},
		{"darwin", "arm64", ""},
	}
	for _, c := range cases {
		got := selectAsset(assets, c.goos, c.goarch)
		if c.want == "" {
			if got != nil {
				t.Errorf("selectAsset(%s/%s) = %q, want nil", c.goos, c.goarch, got.Name)
			}
			continue
		}
		if got == nil || got.Name != c.want {
			t.Errorf("selectAsset(%s/%s) = %v, want %q", c.goos, c.goarch, got, c.want)
		}
	}
}

// zipWith builds an in-memory zip archive from name→content pairs.
func zipWith(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range entries {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractExecutable(t *testing.T) {
	t.Run("picks the exe among siblings", func(t *testing.T) {
		archive := zipWith(t, map[string]string{
			"README.md":   "docs",
			"morgue.exe":  "BINARY",
			"morgue.yaml": "config",
		})
		got, err := extractExecutable(archive, "morgue.exe")
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "BINARY" {
			t.Errorf("got %q, want %q", got, "BINARY")
		}
	})

	t.Run("ignores a directory prefix", func(t *testing.T) {
		archive := zipWith(t, map[string]string{"morgue_windows_amd64/morgue.exe": "BINARY"})
		got, err := extractExecutable(archive, "morgue.exe")
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "BINARY" {
			t.Errorf("got %q, want %q", got, "BINARY")
		}
	})

	t.Run("missing entry errors", func(t *testing.T) {
		archive := zipWith(t, map[string]string{"other.exe": "x"})
		if _, err := extractExecutable(archive, "morgue.exe"); err == nil {
			t.Error("want error for missing entry")
		}
	})

	t.Run("empty entry errors", func(t *testing.T) {
		archive := zipWith(t, map[string]string{"morgue.exe": ""})
		if _, err := extractExecutable(archive, "morgue.exe"); err == nil {
			t.Error("want error for empty entry")
		}
	})

	t.Run("not a zip errors", func(t *testing.T) {
		if _, err := extractExecutable([]byte("not a zip"), "morgue.exe"); err == nil {
			t.Error("want error for malformed archive")
		}
	})
}

func TestReplaceExecutable(t *testing.T) {
	t.Run("swaps in the new binary", func(t *testing.T) {
		dir := t.TempDir()
		exe := filepath.Join(dir, "morgue.exe")
		if err := os.WriteFile(exe, []byte("OLD"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := replaceExecutable(exe, []byte("NEW")); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Clean(exe))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "NEW" {
			t.Errorf("got %q, want %q", got, "NEW")
		}
	})

	t.Run("failure before the swap leaves the original in place", func(t *testing.T) {
		dir := t.TempDir()
		exe := filepath.Join(dir, "sub", "morgue.exe")
		// Parent directory does not exist → CreateTemp fails before anything moves.
		if err := replaceExecutable(exe, []byte("NEW")); err == nil {
			t.Fatal("want error when the target directory is missing")
		}
		if _, err := os.Stat(exe + oldSuffix); !os.IsNotExist(err) {
			t.Errorf("a failed update must not leave %s behind", exe+oldSuffix)
		}
	})

	t.Run("rolls back when the install rename fails", func(t *testing.T) {
		dir := t.TempDir()
		exe := filepath.Join(dir, "morgue.exe")
		if err := os.WriteFile(exe, []byte("OLD"), 0o755); err != nil {
			t.Fatal(err)
		}

		// Fail only the rename that moves the new binary into place — the window
		// where the running binary has already been moved aside.
		calls := 0
		orig := renameFile
		renameFile = func(from, to string) error {
			calls++
			if calls == 2 {
				return os.ErrPermission
			}
			return orig(from, to)
		}
		t.Cleanup(func() { renameFile = orig })

		if err := replaceExecutable(exe, []byte("NEW")); err == nil {
			t.Fatal("want error when the new binary cannot be installed")
		}
		got, err := os.ReadFile(filepath.Clean(exe))
		if err != nil {
			t.Fatalf("original binary was not rolled back: %v", err)
		}
		if string(got) != "OLD" {
			t.Errorf("got %q at %s, want the original %q", got, exe, "OLD")
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.Name() != "morgue.exe" {
				t.Errorf("leftover file after failed update: %s", e.Name())
			}
		}
	})
}
