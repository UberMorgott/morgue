package engine

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/recipe"
	"github.com/UberMorgott/morgue/internal/recon"
	"github.com/UberMorgott/morgue/internal/scanner"
)

type referenceCaptureRecipe struct{ refs []string }

func (*referenceCaptureRecipe) Name() string             { return "reference-capture" }
func (*referenceCaptureRecipe) Description() string      { return "Capture reference paths" }
func (*referenceCaptureRecipe) Match(*recon.Result) bool { return true }
func (*referenceCaptureRecipe) Steps() []recipe.StepInfo { return nil }
func (*referenceCaptureRecipe) RequiredTools() []string  { return nil }
func (r *referenceCaptureRecipe) Execute(ctx *recipe.Context) error {
	r.refs = append([]string(nil), ctx.ReferencePaths...)
	return nil
}

func TestRecipeExecutionForwardsReferencePaths(t *testing.T) {
	for _, filtered := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "filtered"}[filtered], func(t *testing.T) {
			e := New(config.Default(), t.TempDir())
			refs := []string{filepath.Join(t.TempDir(), "Managed"), filepath.Join(t.TempDir(), "core")}
			opts := &Options{Output: t.TempDir(), ReferencePaths: refs}
			r := &referenceCaptureRecipe{}
			path := filepath.Join(t.TempDir(), "App.dll")
			em := emitter{ch: make(chan PipelineEvent, 100)}
			var result TargetResult
			if filtered {
				result = e.executeRecipeWithFilter(context.Background(), path, "App", opts, r, recon.Result{Path: path}, scanner.TargetGroup{}, em, "strings")
			} else {
				result = e.executeRecipe(context.Background(), path, "App", opts, r, recon.Result{Path: path}, scanner.TargetGroup{}, em)
			}
			if result.Error != nil {
				t.Fatal(result.Error)
			}
			if !reflect.DeepEqual(r.refs, refs) {
				t.Fatalf("recipe refs = %q, want %q", r.refs, refs)
			}
		})
	}
}
