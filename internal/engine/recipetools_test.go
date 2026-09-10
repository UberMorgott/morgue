package engine

import (
	"reflect"
	"testing"

	"github.com/UberMorgott/morgue/internal/recipe"
	"github.com/UberMorgott/morgue/internal/recon"
)

// stubRecipe is a minimal recipe.Recipe carrying a fixed RequiredTools list.
type stubRecipe struct{ tools []string }

func (s stubRecipe) Name() string                  { return "stub" }
func (s stubRecipe) Description() string           { return "stub" }
func (s stubRecipe) Match(*recon.Result) bool      { return false }
func (s stubRecipe) Steps() []recipe.StepInfo      { return nil }
func (s stubRecipe) RequiredTools() []string       { return s.tools }
func (s stubRecipe) Execute(*recipe.Context) error { return nil }

func TestRecipeTools(t *testing.T) {
	unity := []string{"ilspycmd", "strings", "assetripper", "assetstudiomod"}

	tests := []struct {
		name       string
		tools      []string
		skipAssets bool
		want       []string
	}{
		{"keeps everything by default", unity, false, unity},
		{"drops asset tools when skipping", unity, true, []string{"ilspycmd", "strings"}},
		{"non-unity recipe unaffected", []string{"ghidra"}, true, []string{"ghidra"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := recipeTools(stubRecipe{tools: tt.tools}, tt.skipAssets)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("recipeTools(skipAssets=%v) = %v, want %v", tt.skipAssets, got, tt.want)
			}
		})
	}
}
