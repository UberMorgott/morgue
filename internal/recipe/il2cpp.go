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

// IL2CPP handles Unity IL2CPP builds.
// It extracts metadata via Il2CppDumper and decompiles the resulting dummy
// assemblies with ilspycmd to produce readable C# source.
type IL2CPP struct{}

func init() {
	Register(&IL2CPP{})
}

func (i *IL2CPP) Name() string        { return "unity-il2cpp" }
func (i *IL2CPP) Description() string { return "Reverse-engineer Unity IL2CPP build" }

func (i *IL2CPP) Match(r *recon.Result) bool {
	return r.Kind == recon.UnityIL2CPP
}

func (i *IL2CPP) Steps() []StepInfo {
	return []StepInfo{
		{Name: "Copy originals", Required: false},
		{Name: "Extract metadata", Required: true},
		{Name: "Decompile metadata assemblies", Required: true},
		{Name: "Extract strings", Required: false},
	}
}

func (i *IL2CPP) RequiredTools() []string {
	return []string{"il2cppdumper", "ilspycmd", "strings"}
}

func (i *IL2CPP) Execute(ctx *Context) error {
	// The IL2CPP group contains both GameAssembly.dll and global-metadata.dat.
	// The pipeline iterates each file separately. We only process when the
	// target is GameAssembly.dll; skip global-metadata.dat silently.
	baseName := strings.ToLower(filepath.Base(ctx.Target))
	if baseName == "global-metadata.dat" {
		// Report all steps as skipped — companion file, processed via GameAssembly.dll
		steps := i.Steps()
		for idx := range steps {
			if ctx.Progress != nil {
				ctx.Progress <- StepProgress{
					Step: idx, Total: len(steps), Name: steps[idx].Name,
					Status: Skipped,
				}
			}
		}
		return nil
	}

	// Find the companion global-metadata.dat
	metadataPath := findGlobalMetadata(filepath.Dir(ctx.Target))
	if metadataPath == "" {
		return fmt.Errorf("could not find global-metadata.dat under %s", filepath.Dir(ctx.Target))
	}

	steps := i.Steps()
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

	// Step 0: Copy originals (only when keeping intermediates)
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
		if err := copyFile(metadataPath, filepath.Join(origDir, filepath.Base(metadataPath))); err != nil {
			report(0, Failed, time.Since(start), err, "")
			return err
		}
		log(fmt.Sprintf("Copied GameAssembly.dll and %s", filepath.Base(metadataPath)))
		report(0, Success, time.Since(start), nil, "")
	} else {
		report(0, Skipped, 0, nil, "")
	}

	// Step 1: Extract metadata with Il2CppDumper
	report(1, Running, 0, nil, "il2cppdumper")
	start = time.Now()

	dumperPath, err := ctx.Tools.Resolve("il2cppdumper")
	if err != nil {
		report(1, Failed, time.Since(start), err, "il2cppdumper")
		return fmt.Errorf("il2cppdumper not available: %w", err)
	}

	metaDir := filepath.Join(ctx.Output, "metadata")
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		report(1, Failed, time.Since(start), err, "il2cppdumper")
		return err
	}

	logTool("il2cppdumper", fmt.Sprintf("Running Il2CppDumper: %s + %s", filepath.Base(ctx.Target), filepath.Base(metadataPath)))
	dumperLastLog := time.Now().Add(-2 * time.Second)
	result, err := util.RunCmdStreamingWithStdin(ctx.Ctx, dumperPath, []string{
		ctx.Target,
		metadataPath,
		metaDir,
	}, "", strings.NewReader("\r\n"), func(line string) {
		if time.Since(dumperLastLog) >= time.Second && strings.TrimSpace(line) != "" {
			logTool("il2cppdumper", fmt.Sprintf("Il2CppDumper: %s", strings.TrimSpace(line)))
			dumperLastLog = time.Now()
		}
	})

	exitCode := -1
	if result != nil {
		exitCode = result.ExitCode
	}

	// Il2CppDumper crashes on Console.ReadKey() after completing work — check output instead of exit code
	dummyDllDir := filepath.Join(metaDir, "DummyDll")
	if _, statErr := os.Stat(dummyDllDir); os.IsNotExist(statErr) {
		stderr := ""
		if result != nil && result.Stderr != "" {
			stderr = result.Stderr
		}
		errMsg := fmt.Errorf("Il2CppDumper failed (exit %d): %s", exitCode, stderr)
		report(1, Failed, time.Since(start), errMsg, "il2cppdumper")
		return errMsg
	}

	// Count outputs for logging
	dummyDlls := countFiles(dummyDllDir, ".dll")
	logTool("il2cppdumper", fmt.Sprintf("Il2CppDumper produced %d dummy assemblies", dummyDlls))
	reportCount(1, time.Since(start), "il2cppdumper", dummyDlls, "assemblies")

	// Step 2: Decompile metadata assemblies with ilspycmd
	report(2, Running, 0, nil, "ilspycmd")
	start = time.Now()

	ilspyPath, err := ctx.Tools.Resolve("ilspycmd")
	if err != nil {
		report(2, Failed, time.Since(start), err, "ilspycmd")
		return fmt.Errorf("ilspycmd not available: %w", err)
	}

	srcDir := filepath.Join(ctx.Output, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		report(2, Failed, time.Since(start), err, "ilspycmd")
		return err
	}

	// Build optional language-version args for ilspycmd
	var langVerArgs []string
	if ctx.Config.CSharpLanguageVersion != "Auto" && ctx.Config.CSharpLanguageVersion != "" {
		langVerArgs = []string{"--language-version", ctx.Config.CSharpLanguageVersion}
	}

	// Find all DLLs in DummyDll/, filter out system/engine libs
	dlls, err := filepath.Glob(filepath.Join(dummyDllDir, "*.dll"))
	if err != nil || len(dlls) == 0 {
		errMsg := fmt.Errorf("no DLLs found in DummyDll/")
		report(2, Failed, time.Since(start), errMsg, "ilspycmd")
		return errMsg
	}

	succeeded := 0
	failed := 0
	skipped := 0
	// Pre-count non-system DLLs for progress reporting
	totalUserDlls := 0
	for _, dll := range dlls {
		if !isSystemDll(filepath.Base(dll)) {
			totalUserDlls++
		}
	}
	for _, dll := range dlls {
		dllName := filepath.Base(dll)
		if isSystemDll(dllName) {
			skipped++
			continue
		}

		// Pause check between decompilations
		if ctx.Pause != nil {
			if err := ctx.Pause.WaitIfPaused(ctx.Ctx); err != nil {
				return err
			}
		}

		baseName := strings.TrimSuffix(dllName, filepath.Ext(dllName))
		outDir := filepath.Join(srcDir, baseName)
		os.MkdirAll(outDir, 0755)

		logTool("ilspycmd", fmt.Sprintf("Decompiling %s...", dllName))
		decompArgs := append([]string{"-p", "-o", outDir, dll}, langVerArgs...)
		res, runErr := util.RunCmd(ctx.Ctx, ilspyPath, decompArgs, "")

		ec := -1
		if res != nil {
			ec = res.ExitCode
		}

		if runErr != nil || ec != 0 {
			// Retry without project mode
			os.RemoveAll(outDir)
			os.MkdirAll(outDir, 0755)
			retryArgs := append([]string{"-o", outDir, dll}, langVerArgs...)
			res, runErr = util.RunCmd(ctx.Ctx, ilspyPath, retryArgs, "")
			ec = -1
			if res != nil {
				ec = res.ExitCode
			}
			if runErr != nil || ec != 0 {
				msg := fmt.Sprintf("  Failed to decompile %s (exit %d)", dllName, ec)
				if res != nil && res.Stderr != "" {
					msg += "\n" + strings.TrimSpace(res.Stderr)
				}
				logTool("ilspycmd", msg)
				failed++
				continue
			}
		}
		succeeded++
		if ctx.Progress != nil {
			ctx.Progress <- StepProgress{
				Step: 2, Total: total, Name: steps[2].Name,
				Tool: "ilspycmd", Status: Running,
				Count: succeeded, CountTotal: totalUserDlls, Unit: "assemblies",
			}
		}
		logTool("ilspycmd", fmt.Sprintf("Decompiled %d/%d assemblies", succeeded, len(dlls)-skipped))
	}

	logTool("ilspycmd", fmt.Sprintf("Decompiled %d assemblies (%d failed, %d system skipped)", succeeded, failed, skipped))

	if succeeded == 0 && failed > 0 {
		errMsg := fmt.Errorf("all %d assembly decompilations failed", failed)
		report(2, Failed, time.Since(start), errMsg, "ilspycmd")
		return errMsg
	}
	reportCount(2, time.Since(start), "ilspycmd", succeeded, "assemblies")

	// Step 3: Extract strings from GameAssembly.dll
	report(3, Running, 0, nil, "strings")
	start = time.Now()
	stringsPath, err := ctx.Tools.Resolve("strings")
	if err != nil {
		logTool("strings", fmt.Sprintf("strings tool not available: %v", err))
		report(3, Skipped, time.Since(start), nil, "strings")
	} else {
		stringsOut := filepath.Join(ctx.Output, "strings.txt")
		strLineCount := 0
		strLastLog := time.Now().Add(-2 * time.Second)
		strLastProgress := time.Now().Add(-2 * time.Second)
		res, _ := util.RunCmdStreaming(ctx.Ctx, stringsPath, []string{"-nobanner", "-accepteula", ctx.Target}, "", func(line string) {
			strLineCount++
			if strLineCount%100 == 0 && time.Since(strLastLog) >= time.Second {
				logTool("strings", fmt.Sprintf("Extracting strings: %d so far...", strLineCount))
				strLastLog = time.Now()
			}
			if time.Since(strLastProgress) >= time.Second {
				if ctx.Progress != nil {
					ctx.Progress <- StepProgress{
						Step: 3, Total: total, Name: steps[3].Name,
						Tool: "strings", Status: Running,
						Count: strLineCount, Unit: "strings",
					}
				}
				strLastProgress = time.Now()
			}
		})
		if res != nil {
			os.WriteFile(stringsOut, []byte(res.Stdout), 0644)
			logTool("strings", fmt.Sprintf("Extracted %d strings from GameAssembly.dll", strLineCount))
		}
		// Analyze and structure strings
		analyzeStrings(stringsOut, filepath.Join(ctx.Output, "strings.json"))
		strCount := countLines(stringsOut)
		reportCount(3, time.Since(start), "strings", strCount, "strings")
	}

	return nil
}

// findGlobalMetadata searches for global-metadata.dat under the given root directory.
func findGlobalMetadata(root string) string {
	var found string
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Base(path), "global-metadata.dat") {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// isSystemDll returns true for DLLs that should not be decompiled
// (system libraries, Unity engine, .NET BCL).
func isSystemDll(name string) bool {
	lower := strings.ToLower(name)

	// .NET BCL / runtime
	if lower == "mscorlib.dll" || lower == "netstandard.dll" {
		return true
	}
	if strings.HasPrefix(lower, "system.") || strings.HasPrefix(lower, "microsoft.") {
		return true
	}

	// Unity engine assemblies
	if strings.HasPrefix(lower, "unityengine.") || strings.HasPrefix(lower, "unity.") {
		return true
	}
	if lower == "unityengine.dll" {
		return true
	}

	// Mono runtime
	if strings.HasPrefix(lower, "mono.") {
		return true
	}

	return false
}

// countFiles counts files with a given extension in a directory (non-recursive).
func countFiles(dir, ext string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ext) {
			count++
		}
	}
	return count
}
