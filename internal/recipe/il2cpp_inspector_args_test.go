package recipe

import (
	"slices"
	"strings"
	"testing"
)

func TestInspectorArgs(t *testing.T) {
	args := inspectorArgs("C:/g/GameAssembly.dll", "C:/g/global-metadata.dat", "D:/out/dump")

	// First token must be the `process` subcommand.
	if len(args) == 0 || args[0] != "process" {
		t.Fatalf("args must start with `process`, got %v", args)
	}
	// Positional inputs (binary + metadata) must precede the -o output flag.
	if args[1] != "C:/g/GameAssembly.dll" || args[2] != "C:/g/global-metadata.dat" {
		t.Fatalf("positional inputs wrong: %v", args)
	}

	joined := strings.Join(args, " ")
	for _, want := range []string{
		"process C:/g/GameAssembly.dll C:/g/global-metadata.dat",
		"-o D:/out/dump",
		"-s",
		"-d",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args missing %q in %q", want, joined)
		}
	}

	// The obsolete v1 CLI flags must be gone.
	for _, banned := range []string{"-i", "--select-outputs", "-c"} {
		for _, a := range args {
			if a == banned {
				t.Fatalf("obsolete flag %q present in %v", banned, args)
			}
		}
	}
}

func TestInspectorEnv(t *testing.T) {
	env := inspectorEnv("D:/runtimes/dotnet-aspnet10", "D:/out/.tmp")
	wantPairs := map[string]string{
		"DOTNET_ROOT": "D:/runtimes/dotnet-aspnet10",
		"TEMP":        "D:/out/.tmp",
		"TMP":         "D:/out/.tmp",
	}
	for k, v := range wantPairs {
		if !hasEnv(env, k, v) {
			t.Fatalf("env missing %s=%s in %v", k, v, env)
		}
	}
}

func hasEnv(env []string, key, val string) bool {
	return slices.Contains(env, key+"="+val)
}
