package apitoken

import (
	"path/filepath"
	"testing"

	"github.com/UberMorgott/morgue/internal/util"
)

func TestPathDefaultsToPortableDataDir(t *testing.T) {
	t.Setenv(EnvPath, "")
	p, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(util.BaseDir(), "data", "api-token"); p != want {
		t.Errorf("Path() = %q, want %q", p, want)
	}
}

func TestPathEnvOverride(t *testing.T) {
	want := filepath.Join(t.TempDir(), "tok")
	t.Setenv(EnvPath, want)
	if p, _ := Path(); p != want {
		t.Errorf("Path() = %q, want override %q", p, want)
	}
}

func TestWriteRead(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "api-token")
	t.Setenv(EnvPath, p)
	if err := Write(p, "abc123"); err != nil {
		t.Fatal(err)
	}
	if tok, err := Read(); err != nil || tok != "abc123" {
		t.Errorf("Read() = %q, %v", tok, err)
	}
}
