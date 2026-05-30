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

// DotnetConfuserEx handles ConfuserEx-obfuscated .NET assemblies.
type DotnetConfuserEx struct{}

func init() {
	RegisterFirst(&DotnetConfuserEx{})
}

func (d *DotnetConfuserEx) Name() string        { return "dotnet-confuserex" }
func (d *DotnetConfuserEx) Description() string { return "Deobfuscate ConfuserEx-protected .NET assembly" }

func (d *DotnetConfuserEx) Match(r *recon.Result) bool {
	if r.Kind != recon.Managed {
		return false
	}
	lower := strings.ToLower(r.Obfuscator)
	return strings.Contains(lower, "confuserex") || strings.Contains(lower, "confuser")
}

func (d *DotnetConfuserEx) Steps() []StepInfo {
	return []StepInfo{
		{Name: "Copy original", Required: false},
		{Name: "NoFuserEx fast-path", Required: false},
		{Name: "Unpack", Required: false},
		{Name: "String decrypt", Required: false},
		{Name: "Control flow + rename", Required: false},
		{Name: "Proxy removal", Required: false},
		{Name: "Extract strings", Required: false},
		{Name: "Extract embedded", Required: false},
		{Name: "Decompile", Required: true},
		{Name: "Build indexes", Required: false},
	}
}

func (d *DotnetConfuserEx) RequiredTools() []string {
	return []string{"ilspycmd", "strings", "de4dot-cex", "nofuserex", "confuserex-killer", "proxycall-remover"}
}

func (d *DotnetConfuserEx) Execute(ctx *Context) error {
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

	interDir := filepath.Join(ctx.Output, "intermediate")
	os.MkdirAll(interDir, 0755)

	// current tracks the working copy through the pipeline
	current := ctx.Target

	// Step 0: Copy original. Always persisted (a single copy of the target is
	// cheap and valuable for reproducibility) — kept consistent across recipes.
	var start time.Time
	report(0, Running, 0, nil, "")
	start = time.Now()
	origDir := filepath.Join(ctx.Output, "original")
	os.MkdirAll(origDir, 0755)
	origCopy := filepath.Join(origDir, filepath.Base(ctx.Target))
	if err := copyFile(ctx.Target, origCopy); err != nil {
		report(0, Failed, time.Since(start), err, "")
		return err
	}
	report(0, Success, time.Since(start), nil, "")

	// Step 1: NoFuserEx fast-path (anti-tamper removal)
	current = d.runToolStep(ctx, 1, current, interDir, "nofuserex", func(toolPath, input, output string) error {
		r, err := util.RunCmd(ctx.Ctx, toolPath, []string{input, "-o", output}, "")
		if err != nil {
			return err
		}
		if r.ExitCode != 0 {
			return fmt.Errorf("nofuserex exit %d: %s", r.ExitCode, r.Stderr)
		}
		return nil
	}, report, logTool)

	// Step 2: Unpack (resource/constant unpacking)
	current = d.runToolStep(ctx, 2, current, interDir, "confuserex-killer", func(toolPath, input, output string) error {
		r, err := util.RunCmd(ctx.Ctx, toolPath, []string{input, "-o", output}, "")
		if err != nil {
			return err
		}
		if r.ExitCode != 0 {
			return fmt.Errorf("unpacker exit %d: %s", r.ExitCode, r.Stderr)
		}
		return nil
	}, report, logTool)

	// Step 3: String decrypt (de4dot-cex --strtyp delegate)
	current = d.runToolStep(ctx, 3, current, interDir, "de4dot-cex", func(toolPath, input, output string) error {
		r, err := util.RunCmd(ctx.Ctx, toolPath, []string{input, "--strtyp", "delegate", "-o", output}, "")
		if err != nil {
			return err
		}
		if r.ExitCode != 0 {
			return fmt.Errorf("de4dot-cex exit %d: %s", r.ExitCode, r.Stderr)
		}
		return nil
	}, report, logTool)

	// Step 4: Control flow + rename (de4dot-cex second pass)
	current = d.runToolStep(ctx, 4, current, interDir, "de4dot-cex", func(toolPath, input, output string) error {
		r, err := util.RunCmd(ctx.Ctx, toolPath, []string{input, "--un-name", "!^<", "-o", output}, "")
		if err != nil {
			return err
		}
		if r.ExitCode != 0 {
			return fmt.Errorf("de4dot-cex exit %d: %s", r.ExitCode, r.Stderr)
		}
		return nil
	}, report, logTool)

	// Step 5: Proxy removal
	current = d.runToolStep(ctx, 5, current, interDir, "proxycall-remover", func(toolPath, input, output string) error {
		r, err := util.RunCmd(ctx.Ctx, toolPath, []string{input, "-o", output}, "")
		if err != nil {
			return err
		}
		if r.ExitCode != 0 {
			return fmt.Errorf("proxycall-remover exit %d: %s", r.ExitCode, r.Stderr)
		}
		return nil
	}, report, logTool)

	// Step 6: Extract strings
	report(6, Running, 0, nil, "strings")
	start = time.Now()
	stringsPath, err := ctx.Tools.Resolve("strings")
	if err != nil {
		logTool("strings", fmt.Sprintf("strings tool not available: %v", err))
		report(6, Skipped, time.Since(start), nil, "strings")
	} else {
		stringsOut := filepath.Join(ctx.Output, "strings.txt")
		strLineCount := 0
		strLastLog := time.Now().Add(-2 * time.Second)
		strLastProgress := time.Now().Add(-2 * time.Second)
		r, _ := util.RunCmdStreaming(ctx.Ctx, stringsPath, []string{"-nobanner", "-accepteula", current}, "", func(line string) {
			strLineCount++
			if strLineCount%100 == 0 && time.Since(strLastLog) >= time.Second {
				logTool("strings", fmt.Sprintf("Extracting strings: %d so far...", strLineCount))
				strLastLog = time.Now()
			}
			if time.Since(strLastProgress) >= time.Second {
				if ctx.Progress != nil {
					ctx.Progress <- StepProgress{
						Step: 6, Total: total, Name: steps[6].Name,
						Tool: "strings", Status: Running,
						Count: strLineCount, Unit: "strings",
					}
				}
				strLastProgress = time.Now()
			}
		})
		if r != nil {
			os.WriteFile(stringsOut, []byte(r.Stdout), 0644)
		}
		// Analyze and structure strings
		analyzeStrings(stringsOut, filepath.Join(ctx.Output, "strings.json"))
		strCount := countLines(stringsOut)
		reportCount(6, time.Since(start), "strings", strCount, "strings")
	}

	// Step 7: Extract embedded (best-effort, scan for costura etc.)
	report(7, Running, 0, nil, "")
	start = time.Now()
	logTool("ilspycmd", "Embedded extraction: scanning for Costura.Fody resources")
	report(7, Skipped, time.Since(start), nil, "") // placeholder — needs specialized logic

	// Step 8: Decompile
	report(8, Running, 0, nil, "ilspycmd")
	start = time.Now()
	ilspyPath, err := ctx.Tools.Resolve("ilspycmd")
	if err != nil {
		report(8, Failed, time.Since(start), err, "ilspycmd")
		return fmt.Errorf("ilspycmd not available: %w", err)
	}
	srcDir := filepath.Join(ctx.Output, "src")
	os.MkdirAll(srcDir, 0755)
	ilspyArgs := []string{"-p", "-o", srcDir, current}
	if ctx.Config.CSharpLanguageVersion != "Auto" && ctx.Config.CSharpLanguageVersion != "" {
		ilspyArgs = append(ilspyArgs, "--language-version", ctx.Config.CSharpLanguageVersion)
	}
	result, runErr := util.RunCmd(ctx.Ctx, ilspyPath, ilspyArgs, "")
	exitCode := -1
	if result != nil {
		exitCode = result.ExitCode
	}
	if runErr != nil || exitCode != 0 {
		stderr := ""
		if result != nil && result.Stderr != "" {
			stderr = result.Stderr
		}
		execErr := fmt.Errorf("ilspycmd failed (exit %d): %s", exitCode, stderr)
		report(8, Failed, time.Since(start), execErr, "ilspycmd")
		return execErr
	}
	csCount := countFilesWithExt(srcDir, ".cs")
	reportCount(8, time.Since(start), "ilspycmd", csCount, "types")

	// Step 9: Build indexes
	report(9, Running, 0, nil, "")
	start = time.Now()
	logTool("ilspycmd", "Building indexes for decompiled output")
	if _, statErr := os.Stat(srcDir); statErr != nil {
		logTool("ilspycmd", "No source to index — ilspycmd produced no src/ output, skipping")
		report(9, Skipped, time.Since(start), nil, "")
	} else if idx, err := buildIndex(srcDir); err != nil {
		logTool("ilspycmd", fmt.Sprintf("Build indexes failed: %v", err))
		report(9, Failed, time.Since(start), err, "")
	} else {
		logTool("ilspycmd", fmt.Sprintf("Indexed %d source files (%d bytes) -> index.json", idx.FileCount, idx.TotalBytes))
		reportCount(9, time.Since(start), "", idx.FileCount, "files")
	}

	return nil
}

// runToolStep runs a single tool step with fallback (copies as-is on failure).
func (d *DotnetConfuserEx) runToolStep(
	ctx *Context, stepIdx int, current, interDir, toolName string,
	run func(toolPath, input, output string) error,
	report func(int, StepStatus, time.Duration, error, string),
	logTool func(string, string),
) string {
	report(stepIdx, Running, 0, nil, toolName)
	start := time.Now()

	toolPath, err := ctx.Tools.Resolve(toolName)
	if err != nil {
		logTool(toolName, fmt.Sprintf("%s not available: %v", toolName, err))
		report(stepIdx, Skipped, time.Since(start), nil, toolName)
		return current
	}

	output := filepath.Join(interDir, fmt.Sprintf("step%d_%s%s", stepIdx, toolName, filepath.Ext(current)))
	if err := run(toolPath, current, output); err != nil {
		logTool(toolName, fmt.Sprintf("%s failed: %v — using previous stage", toolName, err))
		report(stepIdx, Failed, time.Since(start), err, toolName)
		return current // fallback: keep using previous stage
	}

	// Verify output exists
	if _, err := os.Stat(output); err != nil {
		logTool(toolName, fmt.Sprintf("%s produced no output — using previous stage", toolName))
		report(stepIdx, Failed, time.Since(start), err, toolName)
		return current
	}

	report(stepIdx, Success, time.Since(start), nil, toolName)
	return output
}
