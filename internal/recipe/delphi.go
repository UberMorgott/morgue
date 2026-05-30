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

// Delphi handles Delphi/Borland native binaries.
type Delphi struct{}

func init() {
	Register(&Delphi{})
}

func (d *Delphi) Name() string        { return "delphi" }
func (d *Delphi) Description() string { return "Reverse-engineer Delphi binary" }

func (d *Delphi) Match(r *recon.Result) bool {
	return r.Kind == recon.Native && strings.Contains(strings.ToLower(r.Compiler), "delphi")
}

func (d *Delphi) Steps() []StepInfo {
	return []StepInfo{
		{Name: "Copy original", Required: false},
		{Name: "Extract strings", Required: false},
		{Name: "IDR analysis", Required: false},
		{Name: "Ghidra headless", Required: false},
		{Name: "Build indexes", Required: false},
	}
}

func (d *Delphi) RequiredTools() []string {
	return []string{"strings", "idr", "ghidra"}
}

func (d *Delphi) Execute(ctx *Context) error {
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

	// Step 0: Copy original. Always persisted (a single copy of the target is
	// cheap and valuable for reproducibility) — kept consistent across recipes.
	var start time.Time
	report(0, Running, 0, nil, "")
	start = time.Now()
	origDir := filepath.Join(ctx.Output, "original")
	os.MkdirAll(origDir, 0755)
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
		r, _ := util.RunCmd(ctx.Ctx, stringsPath, []string{"-nobanner", "-accepteula", ctx.Target}, "")
		if r != nil {
			os.WriteFile(stringsOut, []byte(r.Stdout), 0644)
		}
		// Analyze and structure strings
		analyzeStrings(stringsOut, filepath.Join(ctx.Output, "strings.json"))
		strCount := countLines(stringsOut)
		reportCount(1, time.Since(start), "strings", strCount, "strings")
	}

	// Step 2: IDR analysis
	if ctx.Config != nil && !ctx.Config.DelphiIDRAnalysis {
		report(2, Skipped, 0, nil, "idr")
		logTool("idr", "IDR analysis disabled in settings")
	} else {
		report(2, Running, 0, nil, "idr")
		start = time.Now()
		idrPath, err := ctx.Tools.Resolve("idr")
		if err != nil {
			logTool("idr", fmt.Sprintf("IDR not available: %v", err))
			report(2, Skipped, time.Since(start), nil, "idr")
		} else {
			idrOut := filepath.Join(ctx.Output, "idr")
			os.MkdirAll(idrOut, 0755)
			result, _ := util.RunCmd(ctx.Ctx, idrPath, []string{"-a", ctx.Target, "-o", idrOut}, "")
			if result != nil && result.ExitCode != 0 {
				logTool("idr", fmt.Sprintf("IDR failed: exit %d", result.ExitCode))
				report(2, Failed, time.Since(start), fmt.Errorf("IDR exit %d", result.ExitCode), "idr")
			} else {
				report(2, Success, time.Since(start), nil, "idr")
			}
		}
	}

	// Step 3: Ghidra headless
	if ctx.Config != nil && !ctx.Config.DelphiGhidraDecompile {
		report(3, Skipped, 0, nil, "ghidra")
		logTool("ghidra", "Ghidra decompilation disabled in settings")
	} else {
		report(3, Running, 0, nil, "ghidra")
		start = time.Now()
		ghidraPath, err := ctx.Tools.Resolve("ghidra")
		if err != nil {
			logTool("ghidra", fmt.Sprintf("Ghidra not available: %v", err))
			report(3, Skipped, time.Since(start), nil, "ghidra")
		} else {
			srcDir := filepath.Join(ctx.Output, "src")
			funcCount, runErr := runGhidra(ctx.Ctx, ghidraPath, resolveGhidraJava(ctx.Tools), ctx.Target, srcDir,
				func(msg string) { logTool("ghidra", msg) },
				func(name string, count int) {
					if ctx.Progress != nil {
						ctx.Progress <- StepProgress{
							Step: 3, Total: total, Name: name,
							Tool: "ghidra", Status: Running,
							Count: count, Unit: "functions",
						}
					}
				},
			)
			if runErr != nil {
				report(3, Failed, time.Since(start), runErr, "ghidra")
			} else {
				reportCount(3, time.Since(start), "ghidra", funcCount, "functions")
			}
		}
	}

	// Step 4: Build indexes
	report(4, Running, 0, nil, "")
	start = time.Now()
	logTool("ghidra", "Building indexes for decompiled output")
	srcDir := filepath.Join(ctx.Output, "src")
	if _, statErr := os.Stat(srcDir); statErr != nil {
		logTool("ghidra", "No source to index — Ghidra produced no src/ output, skipping")
		report(4, Skipped, time.Since(start), nil, "")
	} else if idx, err := buildIndex(srcDir); err != nil {
		logTool("ghidra", fmt.Sprintf("Build indexes failed: %v", err))
		report(4, Failed, time.Since(start), err, "")
	} else {
		logTool("ghidra", fmt.Sprintf("Indexed %d source files (%d bytes) -> index.json", idx.FileCount, idx.TotalBytes))
		reportCount(4, time.Since(start), "", idx.FileCount, "files")
	}

	return nil
}
