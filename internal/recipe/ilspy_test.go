package recipe

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/UberMorgott/morgue/internal/config"
)

func TestIlspyCommandArgsRetainOriginalReferences(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "game")
	intermediate := filepath.Join(root, "output", "intermediate")
	extracted := filepath.Join(root, "output", "extracted")
	for _, dir := range []string{input, intermediate, extracted} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	ctx := &Context{Target: filepath.Join(input, "Game.dll")}
	stage := filepath.Join(intermediate, "step1.dll")
	child := filepath.Join(extracted, "Dependency.dll")
	for _, tc := range []struct {
		name    string
		options []string
	}{
		{"project", []string{"-p", "-o", "src"}},
		{"flat", []string{"-o", "src"}},
		{"per-type", []string{"-t", "Game.Type", "-o", "src"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := ilspyCommandArgs(ctx, stage, tc.options, child)
			want := append(append([]string{}, tc.options...), "-r", input, "-r", intermediate, "-r", extracted, stage)
			if !reflect.DeepEqual(args, want) {
				t.Fatalf("args = %q, want %q", args, want)
			}
		})
	}
}

func TestIlspyExtractedChildReferences(t *testing.T) {
	root := t.TempDir()
	input, children, rewritten := filepath.Join(root, "game"), filepath.Join(root, "extracted"), filepath.Join(root, "child", "intermediate")
	for _, dir := range []string{input, children, rewritten} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	ctx := &Context{Target: filepath.Join(input, "Host.dll")}
	args := ilspyCommandArgs(ctx, filepath.Join(rewritten, "de4dot.dll"), nil, filepath.Join(children, "Child.dll"), filepath.Join(children, "Sibling.dll"))
	want := []string{"-r", input, "-r", rewritten, "-r", children, filepath.Join(rewritten, "de4dot.dll")}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %q, want %q", args, want)
	}
}

func TestIlspyExplicitReferencesAndOptions(t *testing.T) {
	root := t.TempDir()
	refs := filepath.Join(root, "dependencies with spaces")
	if err := os.Mkdir(refs, 0755); err != nil {
		t.Fatal(err)
	}
	ctx := &Context{
		Target:         filepath.Join(root, "Original.dll"),
		Config:         &config.Config{CSharpLanguageVersion: "CSharp12_0"},
		ReferencePaths: []string{refs, filepath.Join(refs, "."), filepath.Join(root, "missing"), "", root},
	}
	options := make([]string, 2, 32)
	copy(options, []string{"-o", "src"})
	args := ilspyCommandArgs(ctx, ctx.Target, options)
	want := []string{"-o", "src", "--languageversion", "CSharp12_0", "-r", refs, "-r", root, ctx.Target}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %q, want %q", args, want)
	}
	if options[:cap(options)][2] != "" {
		t.Fatal("options backing array was modified")
	}
}
