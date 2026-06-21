package recipe

import (
	"strings"
	"testing"
)

func TestInspectorArgs(t *testing.T) {
	args := inspectorArgs("C:/g/GameAssembly.dll", "C:/g/global-metadata.dat", "D:/out/csdir", "D:/out/dlldir")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"-i C:/g/GameAssembly.dll",
		"-m C:/g/global-metadata.dat",
		"--select-outputs",
		"-c D:/out/csdir/il2cpp.cs",
		"-d D:/out/dlldir",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args missing %q in %q", want, joined)
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
	want := key + "=" + val
	for _, e := range env {
		if e == want {
			return true
		}
	}
	return false
}
