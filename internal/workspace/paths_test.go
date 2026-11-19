package workspace

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/UberMorgott/morgue/internal/runmodel"
)

func TestPathPolicyRelativeAndExternal(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "out")
	gd := filepath.Join(base, "gamedata")
	for _, d := range []string{root, gd} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	pp, err := newPathPolicy(root, gd, true)
	if err != nil {
		t.Fatal(err)
	}
	ok := map[string]string{
		filepath.Join(root, "a", "b.cs"): "a/b.cs",
		`a\b\..\c`:                       "a/c",
		"x/y":                            "x/y",
		root:                             ".",
		"":                               "",
	}
	for in, want := range ok {
		if got, err := pp.relative(in); err != nil || got != want {
			t.Errorf("relative(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"../x", `a\..\..\x`, filepath.Join(base, "other"), gd} {
		if got, err := pp.relative(in); !errors.Is(err, errPathEscape) {
			t.Errorf("relative(%q) = %q, %v; want escape", in, got, err)
		}
	}
	if p, ext, err := pp.artifact(filepath.Join(gd, "Assets"), false); err != nil || !ext || p != filepath.ToSlash(filepath.Join(gd, "Assets")) {
		t.Errorf("gamedata artifact = %q %v %v", p, ext, err)
	}
	if p, ext, err := pp.artifact(filepath.Join(root, "GameData"), true); err != nil || ext || p != "GameData" {
		t.Errorf("in-root artifact claimed external = %q %v %v", p, ext, err)
	}
	if _, _, err := pp.artifact(filepath.Join(base, "elsewhere"), true); !errors.Is(err, errPathEscape) {
		t.Errorf("external outside gamedata: %v", err)
	}
	noGD, _ := newPathPolicy(root, "", true)
	if _, _, err := noGD.artifact(filepath.Join(gd, "x"), true); !errors.Is(err, errPathEscape) {
		t.Errorf("external without explicit gamedata destination: %v", err)
	}
}

// A junction (Windows) or symlink inside the workspace that points outside it
// must not let an artifact path escape.
func TestPathPolicyRejectsReparseEscape(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "out")
	outside := filepath.Join(base, "outside")
	inside := filepath.Join(root, "real")
	for _, d := range []string{root, outside, inside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link := func(name, target string) {
		t.Helper()
		p := filepath.Join(root, name)
		var err error
		if runtime.GOOS == "windows" {
			err = exec.CommandContext(t.Context(), "cmd", "/c", "mklink", "/J", p, target).Run()
		} else {
			err = os.Symlink(target, p)
		}
		if err != nil {
			t.Skipf("cannot create link: %v", err)
		}
	}
	link("escape", outside)
	link("alias", inside)
	pp, err := newPathPolicy(root, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pp.relative("escape/src/a.cs"); !errors.Is(err, errPathEscape) {
		t.Fatalf("junction escape accepted: %v", err)
	}
	if got, err := pp.relative("alias/a.cs"); err != nil || got != "alias/a.cs" {
		t.Fatalf("in-root junction rejected: %q %v", got, err)
	}

	// Through the writer: a junction created after admission still cannot
	// redirect a target output or artifact outside the workspace.
	root2 := filepath.Join(base, "out2")
	r := newRun(t, root2, runA)
	w, err := Open(r.h, r.spec)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	mustAppend(t, w, r.started())
	p := filepath.Join(root2, "esc")
	if runtime.GOOS == "windows" {
		err = exec.CommandContext(t.Context(), "cmd", "/c", "mklink", "/J", p, outside).Run()
	} else {
		err = os.Symlink(outside, p)
	}
	if err != nil {
		t.Skipf("cannot create link: %v", err)
	}
	if err := w.Append(r.discovered("t1", "", filepath.Join(p, "game"))); !errors.Is(err, errPathEscape) {
		t.Fatalf("writer accepted junction escape: %v", err)
	}
	r.seq = 1
	if err := w.Append(r.ev("", "", runmodel.ArtifactWritten{Artifact: runmodel.Artifact{ID: "a", Path: "esc/x", Kind: "asset", HashStatus: "pending"}})); !errors.Is(err, errPathEscape) {
		t.Fatalf("writer accepted artifact junction escape: %v", err)
	}
}
