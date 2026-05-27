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
		{Name: "Copy original", Required: true},
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
	report := func(step int, status StepStatus, dur time.Duration, err error) {
		if ctx.Progress != nil {
			ctx.Progress <- StepProgress{
				Step: step, Total: total, Name: steps[step].Name,
				Status: status, Duration: dur, Error: err,
			}
		}
	}
	log := func(msg string) {
		if ctx.Log != nil {
			ctx.Log <- msg
		}
	}

	interDir := filepath.Join(ctx.Output, "intermediate")
	os.MkdirAll(interDir, 0755)

	// current tracks the working copy through the pipeline
	current := ctx.Target

	// Step 0: Copy original
	report(0, Running, 0, nil)
	start := time.Now()
	origDir := filepath.Join(ctx.Output, "original")
	os.MkdirAll(origDir, 0755)
	origCopy := filepath.Join(origDir, filepath.Base(ctx.Target))
	if err := copyFile(ctx.Target, origCopy); err != nil {
		report(0, Failed, time.Since(start), err)
		return err
	}
	report(0, Success, time.Since(start), nil)

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
	}, report, log)

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
	}, report, log)

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
	}, report, log)

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
	}, report, log)

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
	}, report, log)

	// Step 6: Extract strings
	report(6, Running, 0, nil)
	start = time.Now()
	stringsPath, err := ctx.Tools.Resolve("strings")
	if err != nil {
		log(fmt.Sprintf("strings tool not available: %v", err))
		report(6, Skipped, time.Since(start), nil)
	} else {
		stringsOut := filepath.Join(ctx.Output, "strings.txt")
		r, _ := util.RunCmd(ctx.Ctx, stringsPath, []string{"-nobanner", "-accepteula", current}, "")
		if r != nil {
			os.WriteFile(stringsOut, []byte(r.Stdout), 0644)
		}
		report(6, Success, time.Since(start), nil)
	}

	// Step 7: Extract embedded (best-effort, scan for costura etc.)
	report(7, Running, 0, nil)
	start = time.Now()
	log("Embedded extraction: scanning for Costura.Fody resources")
	report(7, Skipped, time.Since(start), nil) // placeholder — needs specialized logic

	// Step 8: Decompile
	report(8, Running, 0, nil)
	start = time.Now()
	ilspyPath, err := ctx.Tools.Resolve("ilspycmd")
	if err != nil {
		report(8, Failed, time.Since(start), err)
		return fmt.Errorf("ilspycmd not available: %w", err)
	}
	srcDir := filepath.Join(ctx.Output, "src")
	os.MkdirAll(srcDir, 0755)
	result, runErr := util.RunCmd(ctx.Ctx, ilspyPath, []string{"-p", "-o", srcDir, current}, "")
	exitCode := -1
	if result != nil {
		exitCode = result.ExitCode
	}
	if runErr != nil || exitCode != 0 {
		execErr := fmt.Errorf("ilspycmd failed: exit %d", exitCode)
		report(8, Failed, time.Since(start), execErr)
		return execErr
	}
	report(8, Success, time.Since(start), nil)

	// Step 9: Build indexes
	report(9, Running, 0, nil)
	start = time.Now()
	log("Building indexes for decompiled output")
	report(9, Skipped, time.Since(start), nil) // placeholder

	return nil
}

// runToolStep runs a single tool step with fallback (copies as-is on failure).
func (d *DotnetConfuserEx) runToolStep(
	ctx *Context, stepIdx int, current, interDir, toolName string,
	run func(toolPath, input, output string) error,
	report func(int, StepStatus, time.Duration, error),
	log func(string),
) string {
	report(stepIdx, Running, 0, nil)
	start := time.Now()

	toolPath, err := ctx.Tools.Resolve(toolName)
	if err != nil {
		log(fmt.Sprintf("%s not available: %v", toolName, err))
		report(stepIdx, Skipped, time.Since(start), nil)
		return current
	}

	output := filepath.Join(interDir, fmt.Sprintf("step%d_%s%s", stepIdx, toolName, filepath.Ext(current)))
	if err := run(toolPath, current, output); err != nil {
		log(fmt.Sprintf("%s failed: %v — using previous stage", toolName, err))
		report(stepIdx, Failed, time.Since(start), err)
		return current // fallback: keep using previous stage
	}

	// Verify output exists
	if _, err := os.Stat(output); err != nil {
		log(fmt.Sprintf("%s produced no output — using previous stage", toolName))
		report(stepIdx, Failed, time.Since(start), err)
		return current
	}

	report(stepIdx, Success, time.Since(start), nil)
	return output
}
