package recipe

import (
	"fmt"

	"github.com/UberMorgott/morgue/internal/recon"
)

// IL2CPP handles Unity IL2CPP builds.
type IL2CPP struct{}

func init() {
	Register(&IL2CPP{})
}

func (i *IL2CPP) Name() string        { return "unity-il2cpp" }
func (i *IL2CPP) Description() string { return "Reverse-engineer Unity IL2CPP build" }

func (i *IL2CPP) Match(r *recon.Result) bool {
	return r.Kind == recon.UnityIL2CPP
}

func (i *IL2CPP) Steps() []StepInfo {
	return []StepInfo{
		{Name: "Copy original", Required: true},
		{Name: "IL2CPP dump", Required: true},
	}
}

func (i *IL2CPP) RequiredTools() []string {
	return nil
}

func (i *IL2CPP) Execute(ctx *Context) error {
	return fmt.Errorf("unity-il2cpp recipe not yet implemented")
}
