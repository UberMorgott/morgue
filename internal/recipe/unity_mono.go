package recipe

import (
	"fmt"

	"github.com/UberMorgott/morgue/internal/recon"
)

// UnityMono handles Unity Mono scripting backend builds.
type UnityMono struct{}

func init() {
	Register(&UnityMono{})
}

func (u *UnityMono) Name() string        { return "unity-mono" }
func (u *UnityMono) Description() string { return "Decompile Unity Mono build" }

func (u *UnityMono) Match(r *recon.Result) bool {
	return r.Kind == recon.UnityMono
}

func (u *UnityMono) Steps() []StepInfo {
	return []StepInfo{
		{Name: "Copy original", Required: true},
		{Name: "Decompile managed DLLs", Required: true},
	}
}

func (u *UnityMono) RequiredTools() []string {
	return []string{"ilspycmd"}
}

func (u *UnityMono) Execute(ctx *Context) error {
	return fmt.Errorf("unity-mono recipe not yet implemented")
}
