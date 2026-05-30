package recipe

import (
	"bytes"
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
		{Name: "Build indexes", Required: false},
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
	logTool := func(tool, msg string) {
		if ctx.Log != nil {
			ctx.Log <- "[" + tool + "] " + msg
		}
	}
	reportCount := func(step int, dur time.Duration, tool string, count int, unit string) {
		if ctx.Progress != nil {
			ctx.Progress <- StepProgress{
				Step: step, Total: total, Name: steps[step].Name,
				Tool: tool, Status: Success, Duration: dur,
				Count: count, Unit: unit,
			}
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
		logTool("strings", fmt.Sprintf("strings tool not available: %v", err))
		report(1, Skipped, time.Since(start), nil, "strings")
	} else {
		stringsOut := filepath.Join(ctx.Output, "strings.txt")
		strLineCount := 0
		strLastLog := time.Now().Add(-2 * time.Second)
		strLastProgress := time.Now().Add(-2 * time.Second)
		result, _ := util.RunCmdStreaming(ctx.Ctx, stringsPath, []string{"-nobanner", "-accepteula", ctx.Target}, "", func(line string) {
			strLineCount++
			if strLineCount%100 == 0 && time.Since(strLastLog) >= time.Second {
				logTool("strings", fmt.Sprintf("Extracting strings: %d so far...", strLineCount))
				strLastLog = time.Now()
			}
			if time.Since(strLastProgress) >= time.Second {
				if ctx.Progress != nil {
					ctx.Progress <- StepProgress{
						Step: 1, Total: total, Name: steps[1].Name,
						Tool: "strings", Status: Running,
						Count: strLineCount, Unit: "strings",
					}
				}
				strLastProgress = time.Now()
			}
		})
		if result != nil {
			os.WriteFile(stringsOut, []byte(result.Stdout), 0644)
		}
		// Analyze and structure strings
		analyzeStrings(stringsOut, filepath.Join(ctx.Output, "strings.json"))
		strCount := countLines(stringsOut)
		reportCount(1, time.Since(start), "strings", strCount, "strings")
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
	ilspyArgs := []string{"-p", "-o", srcDir, ctx.Target}
	if ctx.Config.CSharpLanguageVersion != "Auto" && ctx.Config.CSharpLanguageVersion != "" {
		ilspyArgs = append(ilspyArgs, "--language-version", ctx.Config.CSharpLanguageVersion)
	}
	result, err := util.RunCmd(ctx.Ctx, ilspyPath, ilspyArgs, "")
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
		logTool("ilspycmd", msg)
		os.RemoveAll(srcDir)
		os.MkdirAll(srcDir, 0755)
		retryArgs := []string{"-o", srcDir, ctx.Target}
		if ctx.Config.CSharpLanguageVersion != "Auto" && ctx.Config.CSharpLanguageVersion != "" {
			retryArgs = append(retryArgs, "--language-version", ctx.Config.CSharpLanguageVersion)
		}
		result, err = util.RunCmd(ctx.Ctx, ilspyPath, retryArgs, "")
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
			logTool("ilspycmd", "ilspycmd whole-assembly failed, trying per-type fallback...")
			fallbackOK := perTypeFallback(ctx.Ctx, ilspyPath, ctx.Target, srcDir, func(msg string) { logTool("ilspycmd", msg) })
			if !fallbackOK {
				report(2, Failed, time.Since(start), originalErr, "ilspycmd")
				return originalErr
			}
		} else {
			logTool("ilspycmd", "ilspycmd succeeded without -p (flat file mode)")
		}
	} else {
		logTool("ilspycmd", "ilspycmd succeeded in project mode")
	}
	csCount := countFilesWithExt(srcDir, ".cs")
	reportCount(2, time.Since(start), "ilspycmd", csCount, "types")

	// Step 3: Build indexes
	report(3, Running, 0, nil, "")
	start = time.Now()
	logTool("ilspycmd", "Building indexes for decompiled output")
	if _, statErr := os.Stat(srcDir); statErr != nil {
		logTool("ilspycmd", "No source to index — ilspycmd produced no src/ output, skipping")
		report(3, Skipped, time.Since(start), nil, "")
	} else if idx, err := buildIndex(srcDir); err != nil {
		logTool("ilspycmd", fmt.Sprintf("Build indexes failed: %v", err))
		report(3, Failed, time.Since(start), err, "")
	} else {
		logTool("ilspycmd", fmt.Sprintf("Indexed %d source files (%d bytes) -> index.json", idx.FileCount, idx.TotalBytes))
		reportCount(3, time.Since(start), "", idx.FileCount, "files")
	}

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
		done := succeeded + failed
		if done%10 == 0 || done == len(types) {
			log(fmt.Sprintf("per-type fallback: %d/%d types processed", done, len(types)))
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

// countLines counts newline-separated lines in a file.
func countLines(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return bytes.Count(data, []byte("\n"))
}

// countFilesWithExt recursively counts files with the given extension.
func countFilesWithExt(dir, ext string) int {
	count := 0
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && filepath.Ext(path) == ext {
			count++
		}
		return nil
	})
	return count
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
