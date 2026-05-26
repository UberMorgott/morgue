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
		{Name: "Copy original", Required: true},
		{Name: "Extract strings", Required: false},
		{Name: "IDR analysis", Required: false},
		{Name: "Ghidra headless", Required: false},
	}
}

func (d *Delphi) RequiredTools() []string {
	return []string{"strings", "idr", "ghidra"}
}

func (d *Delphi) Execute(ctx *Context) error {
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
	os.MkdirAll(origDir, 0755)
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
		r, _ := util.RunCmd(ctx.Ctx, stringsPath, []string{"-nobanner", "-accepteula", ctx.Target}, "")
		if r != nil {
			os.WriteFile(stringsOut, []byte(r.Stdout), 0644)
		}
		report(1, Success, time.Since(start), nil)
	}

	// Step 2: IDR analysis
	report(2, Running, 0, nil)
	start = time.Now()
	idrPath, err := ctx.Tools.Resolve("idr")
	if err != nil {
		log(fmt.Sprintf("IDR not available: %v", err))
		report(2, Skipped, time.Since(start), nil)
	} else {
		idrOut := filepath.Join(ctx.Output, "idr")
		os.MkdirAll(idrOut, 0755)
		result, _ := util.RunCmd(ctx.Ctx, idrPath, []string{"-a", ctx.Target, "-o", idrOut}, "")
		if result != nil && result.ExitCode != 0 {
			log(fmt.Sprintf("IDR failed: exit %d", result.ExitCode))
			report(2, Failed, time.Since(start), fmt.Errorf("IDR exit %d", result.ExitCode))
		} else {
			report(2, Success, time.Since(start), nil)
		}
	}

	// Step 3: Ghidra headless
	report(3, Running, 0, nil)
	start = time.Now()
	ghidraPath, err := ctx.Tools.Resolve("ghidra")
	if err != nil {
		log(fmt.Sprintf("Ghidra not available: %v", err))
		report(3, Skipped, time.Since(start), nil)
	} else {
		ghidraOut := filepath.Join(ctx.Output, "ghidra")
		os.MkdirAll(ghidraOut, 0755)
		result, _ := util.RunCmd(ctx.Ctx, ghidraPath, []string{
			ghidraOut, "MorgueProject", "-import", ctx.Target,
			"-postScript", "ExportDecompiled.java",
		}, "")
		if result != nil && result.ExitCode != 0 {
			log(fmt.Sprintf("Ghidra failed: exit %d", result.ExitCode))
			report(3, Failed, time.Since(start), fmt.Errorf("Ghidra exit %d", result.ExitCode))
		} else {
			report(3, Success, time.Since(start), nil)
		}
	}

	return nil
}
