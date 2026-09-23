package recipe

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/UberMorgott/morgue/internal/recon"
	"github.com/UberMorgott/morgue/internal/tools"
	"github.com/UberMorgott/morgue/internal/util"
)

// Java decompiles JVM bytecode (.jar/.war/.class) to .java with Vineflower.
type Java struct{}

func init() {
	Register(&Java{})
}

func (j *Java) Name() string        { return "java" }
func (j *Java) Description() string { return "Decompile Java bytecode with Vineflower" }

func (j *Java) Match(r *recon.Result) bool { return r.Kind == recon.Java }

func (j *Java) Steps() []StepInfo {
	return []StepInfo{
		{Name: "Copy original", Required: false},
		{Name: "Decompile with Vineflower", Required: true},
		{Name: "Build indexes", Required: false},
	}
}

func (j *Java) RequiredTools() []string { return []string{"vineflower"} }

func (j *Java) Execute(ctx *Context) error {
	steps := j.Steps()
	total := len(steps)
	report := func(step int, status StepStatus, dur time.Duration, err error, count int, unit string) {
		if ctx.Progress != nil {
			ctx.Progress <- StepProgress{
				Step: step, Total: total, Name: steps[step].Name, Tool: "vineflower",
				Status: status, Duration: dur, Error: err, Count: count, Unit: unit,
			}
		}
	}
	logTool := func(msg string) {
		if ctx.Log != nil {
			ctx.Log <- "[vineflower] " + msg
		}
	}

	// Step 0: Copy original (kept consistent across recipes).
	report(0, Running, 0, nil, 0, "")
	start := time.Now()
	origDir := filepath.Join(ctx.Output, "original")
	_ = os.MkdirAll(origDir, 0755)
	if err := copyFile(ctx.Target, filepath.Join(origDir, filepath.Base(ctx.Target))); err != nil {
		report(0, Failed, time.Since(start), err, 0, "")
		return err
	}
	report(0, Success, time.Since(start), nil, 0, "")

	// Step 1: Vineflower.
	report(1, Running, 0, nil, 0, "")
	start = time.Now()
	jarPath, err := ctx.Tools.Resolve("vineflower")
	if err != nil {
		err = fmt.Errorf("vineflower not available: %w", err)
		report(1, Failed, time.Since(start), err, 0, "")
		return err
	}
	javaPath, err := ctx.Tools.RuntimePath(tools.RuntimeJava)
	if err != nil {
		err = fmt.Errorf("java runtime not available: %w", err)
		report(1, Failed, time.Since(start), err, 0, "")
		return err
	}
	srcDir := filepath.Join(ctx.Output, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		report(1, Failed, time.Since(start), err, 0, "")
		return err
	}
	classes := 0
	lastReport := time.Now()
	onLine := func(line string) {
		if !strings.Contains(line, "Decompiling class") {
			return
		}
		classes++
		if time.Since(lastReport) >= time.Second {
			lastReport = time.Now()
			report(1, Running, 0, nil, classes, "classes")
		}
	}
	// Breakaway: a large fat jar needs more heap than morgue's own Job Object
	// memory cap allows (the JVM sizes its default heap from physical RAM).
	res, runErr := util.RunCmdStreamingEnvBreakaway(ctx.Ctx, ctx.Tools.RuntimeEnv(), javaPath,
		vineflowerArgs(jarPath, ctx.Target, srcDir), "", onLine)
	if runErr == nil && res != nil && res.ExitCode != 0 {
		runErr = fmt.Errorf("vineflower exit %d: %s", res.ExitCode, lastLine(res.Stderr))
	}
	files := countFilesWithExt(srcDir, ".java")
	if runErr != nil && files == 0 {
		report(1, Failed, time.Since(start), runErr, 0, "")
		return runErr
	}
	if runErr != nil {
		logTool(fmt.Sprintf("finished with errors (%v); keeping %d decompiled files", runErr, files))
		report(1, Warn, time.Since(start), runErr, files, "files")
	} else {
		report(1, Success, time.Since(start), nil, files, "files")
	}

	// Step 2: Build indexes.
	report(2, Running, 0, nil, 0, "")
	start = time.Now()
	idx, err := buildIndex(srcDir)
	if err != nil {
		logTool(fmt.Sprintf("Build indexes failed: %v", err))
		report(2, Failed, time.Since(start), err, 0, "")
		return nil
	}
	report(2, Success, time.Since(start), nil, idx.FileCount, "files")
	return nil
}

// vineflowerArgs builds the java command line. A directory destination makes
// Vineflower write a plain .java tree (not a sources jar).
func vineflowerArgs(jarPath, target, srcDir string) []string {
	return []string{"-jar", jarPath, "--log-level=info", target, srcDir}
}

// lastLine returns the last non-empty line of s (for short error messages).
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
