package recipe

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/UberMorgott/morgue/internal/util"
)

// findDotnetHost returns a dotnet executable capable of running framework-
// dependent .NET tools (a bare .dll). Prefers the 64-bit install in Program
// Files because the `dotnet` on PATH may be an x86 stub without the required
// shared runtime. Returns "" if none works.
func findDotnetHost(ctx context.Context) string {
	var candidates []string
	if pf := os.Getenv("ProgramFiles"); pf != "" {
		candidates = append(candidates, filepath.Join(pf, "dotnet", "dotnet.exe"))
	}
	candidates = append(candidates, `C:\Program Files\dotnet\dotnet.exe`, "dotnet")

	seen := map[string]bool{}
	for _, c := range candidates {
		if seen[c] {
			continue
		}
		seen[c] = true
		if c != "dotnet" {
			if _, err := os.Stat(c); err != nil {
				continue
			}
		}
		r, err := util.RunCmd(ctx, c, []string{"--list-runtimes"}, "")
		if err == nil && r != nil && r.ExitCode == 0 && strings.TrimSpace(r.Stdout) != "" {
			return c
		}
	}
	return ""
}

// dotnetExec wraps a resolved tool invocation so framework-dependent .NET tools
// — which resolve to a .dll that cannot be executed directly — run via the
// dotnet host (`dotnet tool.dll <args>`). Non-.dll tools pass through unchanged.
func dotnetExec(ctx context.Context, toolPath string, args []string) (string, []string) {
	if !strings.EqualFold(filepath.Ext(toolPath), ".dll") {
		return toolPath, args
	}
	if host := findDotnetHost(ctx); host != "" {
		return host, append([]string{toolPath}, args...)
	}
	return toolPath, args
}
