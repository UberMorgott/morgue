package recipe

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ilspyCommandArgs keeps reference resolution identical in project, flat and
// per-type mode. Related assemblies include raw extracted siblings, whose
// directories remain relevant after a child is rewritten into intermediate/.
func ilspyCommandArgs(ctx *Context, target string, options []string, related ...string) []string {
	args := append([]string(nil), options...)
	if ctx.Config != nil && ctx.Config.CSharpLanguageVersion != "" && ctx.Config.CSharpLanguageVersion != "Auto" {
		args = append(args, "--languageversion", ctx.Config.CSharpLanguageVersion)
	}
	seen := make(map[string]bool)
	add := func(dir string) {
		if dir == "" {
			return
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			return
		}
		key := abs
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if seen[key] {
			return
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			return
		}
		seen[key] = true
		args = append(args, "-r", abs)
	}
	for _, dir := range ctx.ReferencePaths {
		add(dir)
	}
	if ctx.Target != "" {
		add(filepath.Dir(ctx.Target))
	}
	add(filepath.Dir(target))
	for _, assembly := range related {
		if assembly != "" {
			add(filepath.Dir(assembly))
		}
	}
	return append(args, target)
}
