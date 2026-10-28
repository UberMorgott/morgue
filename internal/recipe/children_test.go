package recipe

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/tools"
)

type childObservation struct {
	discovered  []string
	outcomes    []string
	discoverErr error
}

func (c *childObservation) Discover(input, output, name string) (string, error) {
	c.discovered = append(c.discovered, input)
	return input, c.discoverErr
}
func (c *childObservation) Finish(id, outcome, message string) error {
	c.outcomes = append(c.outcomes, outcome)
	return nil
}

func TestExtractedChildrenAccountedWhenDecompilerMissing(t *testing.T) {
	o := &childObservation{}
	ctx := &Context{Ctx: context.Background(), Output: t.TempDir(), Tools: tools.NewManager(t.TempDir(), config.Default()), Children: o}
	dlls := []string{filepath.Join(ctx.Output, "a.dll"), filepath.Join(ctx.Output, "b.dll")}
	done, err := (&DotnetConfuserEx{}).decompileExtracted(ctx, dlls, func(int, StepStatus, time.Duration, error, string) {}, func(string, string) {})
	if err != nil || done != 0 || len(o.discovered) != 2 || len(o.outcomes) != 2 {
		t.Fatalf("children disappeared: done=%d err=%v observations=%+v", done, err, o)
	}
	for _, outcome := range o.outcomes {
		if outcome != "error" {
			t.Fatalf("missing tool child outcome=%s", outcome)
		}
	}
}

func TestExtractedChildRegistrationFailurePropagates(t *testing.T) {
	want := errors.New("observer failed")
	o := &childObservation{discoverErr: want}
	ctx := &Context{Ctx: context.Background(), Output: t.TempDir(), Tools: tools.NewManager(t.TempDir(), config.Default()), Children: o}
	_, err := (&DotnetConfuserEx{}).decompileExtracted(ctx, []string{"child.dll"}, func(int, StepStatus, time.Duration, error, string) {}, func(string, string) {})
	if !errors.Is(err, want) {
		t.Fatalf("registration failure swallowed: %v", err)
	}
}
