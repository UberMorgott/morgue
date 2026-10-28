package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/UberMorgott/morgue/internal/recipe"
	"github.com/UberMorgott/morgue/internal/recon"
)

func TestRunReferenceFlagsRepeatWithoutSplittingPaths(t *testing.T) {
	cmd := runCmd()
	paths := []string{`C:\game dependencies\core`, `D:\refs, archived`}
	if err := cmd.ParseFlags([]string{"--ref", paths[0], "--ref", paths[1]}); err != nil {
		t.Fatal(err)
	}
	got, err := cmd.Flags().GetStringArray("ref")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, paths) {
		t.Fatalf("refs = %q, want %q", got, paths)
	}
}

type cliReferenceRecipe struct {
	name string
	refs []string
}

func (r *cliReferenceRecipe) Name() string           { return r.name }
func (*cliReferenceRecipe) Description() string      { return "Capture CLI reference paths" }
func (*cliReferenceRecipe) Match(*recon.Result) bool { return false }
func (*cliReferenceRecipe) Steps() []recipe.StepInfo { return nil }
func (*cliReferenceRecipe) RequiredTools() []string  { return nil }
func (r *cliReferenceRecipe) Execute(ctx *recipe.Context) error {
	r.refs = append([]string(nil), ctx.ReferencePaths...)
	return nil
}

func TestRunReferenceFlagsReachRecipe(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "App.dll")
	if err := os.WriteFile(input, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	paths := []string{filepath.Join(root, "refs one"), filepath.Join(root, "refs,two")}
	for _, path := range paths {
		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	r := &cliReferenceRecipe{name: "test-cli-reference-paths-" + filepath.Base(filepath.Dir(root))}
	recipe.Register(r)
	cmd := runCmd()
	cmd.SetArgs([]string{input, "--recipe", r.Name(), "--no-skip", "--quiet", "--output", filepath.Join(root, "out"), "--ref", paths[0], "--ref", paths[1]})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.refs, paths) {
		t.Fatalf("recipe refs = %q, want %q", r.refs, paths)
	}
}
