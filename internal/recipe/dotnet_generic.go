package recipe

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
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
		{Name: "Copy original", Required: true},
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

	// Step 2: Decompile
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
		execErr := fmt.Errorf("ilspycmd failed: exit %d", exitCode)
		report(2, Failed, time.Since(start), execErr)
		return execErr
	}
	report(2, Success, time.Since(start), nil)

	return nil
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
