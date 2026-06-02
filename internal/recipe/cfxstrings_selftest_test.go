package recipe

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/UberMorgott/morgue/internal/util"
)

// TestCfxStringsCorruptionSafety is the committed corruption-safety regression for
// the destructive custom-string-decryptor rewrite path. It builds the embedded
// cfxstrings tool (cached, same as the recipe) and runs its `--selftest`, which
// drives the REAL decode + validation gate (Decrypt + ConfidenceCharWeighted +
// CrossMagicAgrees + the accept decision) over committed (ciphertext, magic,
// plaintext) vectors recorded from the ServerProtocol oracle — no external DLL or
// oracle path. It asserts BOTH directions:
//
//	(a) correct key  -> plaintext matches, gate ACCEPTs (would rewrite): SELFTEST-CORRECT:PASS
//	(b) near-miss key -> gate WITHHOLDs (no rewrite, IL untouched):       SELFTEST-NEARMISS:WITHHELD
//
// Skips (does not fail) when no .NET SDK is available to build the tool.
func TestCfxStringsCorruptionSafety(t *testing.T) {
	d := &DotnetConfuserEx{}
	ctx := &Context{Ctx: context.Background()}

	dotnet := d.resolveDotnetSDK(ctx)
	if dotnet == "" {
		t.Skip("no .NET SDK found (PATH or C:\\Program Files\\dotnet) — cannot build cfxstrings")
	}

	dll, err := d.buildStringsDecryptor(ctx, dotnet, func(string, string) {})
	if err != nil {
		t.Skipf("cfxstrings build unavailable: %v", err)
	}
	if _, statErr := os.Stat(dll); statErr != nil {
		t.Skipf("cfxstrings.dll missing after build: %v", statErr)
	}

	r, runErr := util.RunCmd(ctx.Ctx, dotnet, []string{dll, "--selftest"}, "")
	if runErr != nil {
		t.Fatalf("running cfxstrings --selftest: %v", runErr)
	}
	out := r.Stdout + r.Stderr
	t.Logf("cfxstrings --selftest output:\n%s", strings.TrimSpace(out))

	if r.ExitCode != 0 {
		t.Fatalf("cfxstrings --selftest exited %d (want 0)\noutput:\n%s", r.ExitCode, out)
	}
	// (a) correct key accepted + plaintext matches
	if !strings.Contains(out, "SELFTEST-CORRECT:PASS") {
		t.Errorf("correct-key direction failed: missing SELFTEST-CORRECT:PASS\noutput:\n%s", out)
	}
	// (b) near-miss key withheld (no destructive rewrite on a wrong key)
	if !strings.Contains(out, "SELFTEST-NEARMISS:WITHHELD") {
		t.Errorf("near-miss direction failed: missing SELFTEST-NEARMISS:WITHHELD\noutput:\n%s", out)
	}
}
