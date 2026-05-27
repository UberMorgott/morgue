package recipe

import (
	"fmt"
	"os"
	"path/filepath"
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
		{Name: "Copy original", Required: true},
		{Name: "Extract strings", Required: false},
		{Name: "Decompile managed DLLs", Required: true},
	}
}

func (u *UnityMono) RequiredTools() []string {
	return []string{"ilspycmd", "strings"}
}

func (u *UnityMono) Execute(ctx *Context) error {
	steps := u.Steps()
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

	// Step 0: Copy original
	report(0, Running, 0, nil)
	start := time.Now()
	origDir := filepath.Join(ctx.Output, "original")
	if err := os.MkdirAll(origDir, 0755); err != nil {
		report(0, Failed, time.Since(start), err)
		return err
	}
	if err := copyFile(ctx.Target, filepath.Join(origDir, filepath.Base(ctx.Target))); err != nil {
		report(0, Failed, time.Since(start), err)
		return err
	}
	report(0, Success, time.Since(start), nil)

	// Step 1: Extract strings
	report(1, Running, 0, nil)
	start = time.Now()
	stringsPath, err := ctx.Tools.Resolve("strings")
	if err != nil {
		log(fmt.Sprintf("strings tool not available: %v", err))
		report(1, Skipped, time.Since(start), nil)
	} else {
		stringsOut := filepath.Join(ctx.Output, "strings.txt")
		result, _ := util.RunCmd(ctx.Ctx, stringsPath, []string{"-nobanner", "-accepteula", ctx.Target}, "")
		if result != nil {
			os.WriteFile(stringsOut, []byte(result.Stdout), 0644)
		}
		report(1, Success, time.Since(start), nil)
	}

	// Step 2: Decompile managed DLLs
	report(2, Running, 0, nil)
	start = time.Now()
	ilspyPath, err := ctx.Tools.Resolve("ilspycmd")
	if err != nil {
		report(2, Failed, time.Since(start), err)
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
		log(fmt.Sprintf("ilspycmd project mode failed (exit %d), retrying without -p", exitCode))
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
				report(2, Failed, time.Since(start), originalErr)
				return originalErr
			}
		} else {
			log("ilspycmd succeeded without -p (flat file mode)")
		}
	} else {
		log("ilspycmd succeeded in project mode")
	}
	report(2, Success, time.Since(start), nil)

	return nil
}
