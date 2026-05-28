package recipe

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/UberMorgott/morgue/internal/recon"
	"github.com/UberMorgott/morgue/internal/util"
)

// DotnetGeneric handles clean .NET assemblies (no obfuscation).
type DotnetGeneric struct{}

func init() {
	Register(&DotnetGeneric{})
}

func (d *DotnetGeneric) Name() string        { return "dotnet-generic" }
func (d *DotnetGeneric) Description() string { return "Decompile clean .NET assembly" }

func (d *DotnetGeneric) Match(r *recon.Result) bool {
	return r.Kind == recon.Managed && !r.NeedsDeobfuscation()
}

func (d *DotnetGeneric) Steps() []StepInfo {
	return []StepInfo{
		{Name: "Copy original", Required: false},
		{Name: "Extract strings", Required: false},
		{Name: "Decompile", Required: true},
	}
}

func (d *DotnetGeneric) RequiredTools() []string {
	return []string{"ilspycmd", "strings"}
}

func (d *DotnetGeneric) Execute(ctx *Context) error {
	steps := d.Steps()
	total := len(steps)
	report := func(step int, status StepStatus, dur time.Duration, err error, tool string) {
		if ctx.Progress != nil {
			ctx.Progress <- StepProgress{
				Step: step, Total: total, Name: steps[step].Name,
				Tool: tool, Status: status, Duration: dur, Error: err,
			}
		}
	}
	log := func(msg string) {
		if ctx.Log != nil {
			ctx.Log <- msg
		}
	}

	// Step 0: Copy original (only when keeping intermediates)
	var start time.Time
	if ctx.Config.KeepIntermediates {
		report(0, Running, 0, nil, "")
		start = time.Now()
		origDir := filepath.Join(ctx.Output, "original")
		if err := os.MkdirAll(origDir, 0755); err != nil {
			report(0, Failed, time.Since(start), err, "")
			return err
		}
		if err := copyFile(ctx.Target, filepath.Join(origDir, filepath.Base(ctx.Target))); err != nil {
			report(0, Failed, time.Since(start), err, "")
			return err
		}
		report(0, Success, time.Since(start), nil, "")
	} else {
		report(0, Skipped, 0, nil, "")
	}

	// Step 1: Extract strings
	report(1, Running, 0, nil, "strings")
	start = time.Now()
	stringsPath, err := ctx.Tools.Resolve("strings")
	if err != nil {
		log(fmt.Sprintf("strings tool not available: %v", err))
		report(1, Skipped, time.Since(start), nil, "strings")
	} else {
		stringsOut := filepath.Join(ctx.Output, "strings.txt")
		result, _ := util.RunCmd(ctx.Ctx, stringsPath, []string{"-nobanner", "-accepteula", ctx.Target}, "")
		if result != nil {
			os.WriteFile(stringsOut, []byte(result.Stdout), 0644)
		}
		// Analyze and structure strings
		analyzeStrings(stringsOut, filepath.Join(ctx.Output, "strings.json"))
		report(1, Success, time.Since(start), nil, "strings")
	}

	// Step 2: Decompile
	report(2, Running, 0, nil, "ilspycmd")
	start = time.Now()
	ilspyPath, err := ctx.Tools.Resolve("ilspycmd")
	if err != nil {
		report(2, Failed, time.Since(start), err, "ilspycmd")
		return fmt.Errorf("ilspycmd not available: %w", err)
	}
	srcDir := filepath.Join(ctx.Output, "src")
	os.MkdirAll(srcDir, 0755)
	result, err := util.RunCmd(ctx.Ctx, ilspyPath, []string{"-p", "-o", srcDir, ctx.Target}, "")
	exitCode := -1
	if result != nil {
		exitCode = result.ExitCode
	}
	if err != nil || exitCode != 0 {
		// Project mode failed — retry without -p (flat .cs output, more tolerant)
		msg := fmt.Sprintf("ilspycmd project mode failed (exit %d), retrying without -p", exitCode)
		if result != nil && result.Stderr != "" {
			msg += "\n" + strings.TrimSpace(result.Stderr)
		}
		log(msg)
		os.RemoveAll(srcDir)
		os.MkdirAll(srcDir, 0755)
		result, err = util.RunCmd(ctx.Ctx, ilspyPath, []string{"-o", srcDir, ctx.Target}, "")
		exitCode = -1
		if result != nil {
			exitCode = result.ExitCode
		}
		if err != nil || exitCode != 0 {
			stderr := ""
			if result != nil && result.Stderr != "" {
				stderr = result.Stderr
			}
			originalErr := fmt.Errorf("ilspycmd failed (exit %d): %s", exitCode, stderr)

			// Fallback: per-type decompilation
			log("ilspycmd whole-assembly failed, trying per-type fallback...")
			fallbackOK := perTypeFallback(ctx.Ctx, ilspyPath, ctx.Target, srcDir, log)
			if !fallbackOK {
				report(2, Failed, time.Since(start), originalErr, "ilspycmd")
				return originalErr
			}
		} else {
			log("ilspycmd succeeded without -p (flat file mode)")
		}
	} else {
		log("ilspycmd succeeded in project mode")
	}
	report(2, Success, time.Since(start), nil, "ilspycmd")

	return nil
}

// perTypeFallback enumerates all types in an assembly and decompiles them
// one by one. Returns true if at least one type was decompiled successfully.
func perTypeFallback(ctx context.Context, ilspyPath, target, srcDir string, log func(string)) bool {
	listResult, listErr := util.RunCmd(ctx, ilspyPath, []string{
		"-l", "cisde", "--disable-updatecheck", target,
	}, "")
	if listErr != nil || listResult == nil || listResult.ExitCode != 0 {
		msg := fmt.Sprintf("per-type fallback: failed to list types")
		if listResult != nil && listResult.Stderr != "" {
			msg += "\n" + strings.TrimSpace(listResult.Stderr)
		}
		log(msg)
		return false
	}

	types := parseTypeList(listResult.Stdout)
	if len(types) == 0 {
		log("per-type fallback: no types found in assembly")
		return false
	}

	log(fmt.Sprintf("per-type fallback: found %d types, decompiling individually...", len(types)))
	os.RemoveAll(srcDir)
	os.MkdirAll(srcDir, 0o755)

	succeeded := 0
	failed := 0
	for _, typeName := range types {
		typeResult, _ := util.RunCmd(ctx, ilspyPath, []string{
			"-t", typeName, "-o", srcDir, "--disable-updatecheck", target,
		}, "")
		if typeResult != nil && typeResult.ExitCode == 0 {
			succeeded++
		} else {
			failed++
		}
	}

	if succeeded == 0 {
		log("per-type fallback: all types failed")
		return false
	}

	log(fmt.Sprintf("per-type fallback: %d/%d types decompiled", succeeded, succeeded+failed))
	return true
}

// parseTypeList extracts fully-qualified type names from ilspycmd -l output.
// Each line has the format "TypeKind FullName", e.g. "Class Foo.Bar".
func parseTypeList(output string) []string {
	var types []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "ilspycmd") || strings.HasPrefix(line, "ICSharpCode") {
			continue
		}
		// Strip type-kind prefix: "Class ", "Struct ", "Interface ", "Enum ", "Delegate "
		if idx := strings.IndexByte(line, ' '); idx >= 0 {
			line = line[idx+1:]
		}
		// Skip <Module> and compiler-generated types (e.g. <>c, <>c__DisplayClass).
		// These contain <> which are invalid in Windows filenames, and their code
		// is already included when decompiling the parent type.
		if line == "" || strings.Contains(line, "<") {
			continue
		}
		types = append(types, line)
	}
	return types
}

// copyFile copies src to dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}
