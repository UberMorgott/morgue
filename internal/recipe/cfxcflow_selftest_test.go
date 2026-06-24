package recipe

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/UberMorgott/morgue/internal/util"
)

// TestCfxCflowSelftest is the committed safety regression for the control-flow
// deobfuscation pass. It builds the embedded cfxcflow tool (cached, same as the
// recipe) and runs its `--selftest`, which drives the REAL Detect -> Prove ->
// Apply -> Verify family over in-memory AsmResolver fixtures — no external DLL.
// It asserts all three directions:
//
//	(a) a flattened constant-chain fixture is de-flattened AND stays semantically
//	    equal (pre/post in-proc interpretation match):   SELFTEST-DEFLATTEN:PASS
//	(b) an argument-dependent (runtime-state) fixture is left byte-identical
//	    (no Plan produced, withheld):                     SELFTEST-WITHHELD:PASS
//	(c) the rewritten output reparses/validates:          SELFTEST-VERIFY:PASS
//
// Skips (does not fail) when no .NET SDK is available to build the tool.
func TestCfxCflowSelftest(t *testing.T) {
	d := &DotnetConfuserEx{}
	ctx := &Context{Ctx: context.Background()}

	dotnet := d.resolveDotnetSDK(ctx)
	if dotnet == "" {
		t.Skip("no .NET SDK found (PATH or C:\\Program Files\\dotnet) — cannot build cfxcflow")
	}

	dll, err := d.buildCflowPass(ctx, dotnet, func(string, string) {})
	if err != nil {
		t.Skipf("cfxcflow build unavailable: %v", err)
	}
	if _, statErr := os.Stat(dll); statErr != nil {
		t.Skipf("cfxcflow.dll missing after build: %v", statErr)
	}

	r, runErr := util.RunCmd(ctx.Ctx, dotnet, []string{dll, "--selftest"}, "")
	if runErr != nil {
		t.Fatalf("running cfxcflow --selftest: %v", runErr)
	}
	out := r.Stdout + r.Stderr
	t.Logf("cfxcflow --selftest output:\n%s", strings.TrimSpace(out))

	if r.ExitCode != 0 {
		t.Fatalf("cfxcflow --selftest exited %d (want 0)\noutput:\n%s", r.ExitCode, out)
	}
	for _, want := range []string{
		"SELFTEST-DEFLATTEN:PASS",
		"SELFTEST-WITHHELD:PASS",
		"SELFTEST-VERIFY:PASS",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s\noutput:\n%s", want, out)
		}
	}
}
