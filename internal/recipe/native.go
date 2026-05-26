package recipe

import (
	"fmt"

	"github.com/UberMorgott/morgue/internal/recon"
)

// Native is a catch-all recipe for native binaries.
type Native struct{}

func init() {
	Register(&Native{})
}

func (n *Native) Name() string        { return "native" }
func (n *Native) Description() string { return "Reverse-engineer native binary" }

func (n *Native) Match(r *recon.Result) bool {
	return r.Kind == recon.Native
}

func (n *Native) Steps() []StepInfo {
	return []StepInfo{
		{Name: "Copy original", Required: true},
		{Name: "Disassemble", Required: true},
	}
}

func (n *Native) RequiredTools() []string {
	return []string{"ghidra"}
}

func (n *Native) Execute(ctx *Context) error {
	return fmt.Errorf("native recipe not yet implemented")
}
