package recipe

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/UberMorgott/morgue/internal/recon"
	"github.com/UberMorgott/morgue/internal/util"
)

// UnityMono handles Unity Mono scripting backend builds.
type UnityMono struct{}

func init() {
	Register(&UnityMono{})
}

func (u *UnityMono) Name() string        { return "unity-mono" }
func (u *UnityMono) Description() string { return "Decompile Unity Mono build" }

func (u *UnityMono) Match(r *recon.Result) bool {
	return r.Kind == recon.UnityMono
}

func (u *UnityMono) Steps() []StepInfo {
	return []StepInfo{
		{Name: "Copy original", Required: false},
		{Name: "Extract strings", Required: false},
		{Name: "Decompile managed DLLs", Required: true},
		{Name: "Build indexes", Required: false},
	}
}

func (u *UnityMono) RequiredTools() []string {
	return []string{"ilspycmd", "strings"}
}

func (u *UnityMono) Execute(ctx *Context) error {
	steps := u.Steps()
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

	// Step 0: Copy original. Always persisted (a single copy of the target is
	// cheap and valuable for reproducibility) — kept consistent across recipes.
	var start time.Time
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

	// Step 2: Decompile managed DLLs
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
		ilspyArgs = append(ilspyArgs, "--languageversion", ctx.Config.CSharpLanguageVersion)
	}
	ilspyBin, ilspyRun := dotnetExec(ctx.Ctx, ilspyPath, ilspyArgs)
	result, err := util.RunCmd(ctx.Ctx, ilspyBin, ilspyRun, "")
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
			retryArgs = append(retryArgs, "--languageversion", ctx.Config.CSharpLanguageVersion)
		}
		urb, ura := dotnetExec(ctx.Ctx, ilspyPath, retryArgs)
		result, err = util.RunCmd(ctx.Ctx, urb, ura, "")
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
