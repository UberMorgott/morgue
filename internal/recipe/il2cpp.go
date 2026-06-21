package recipe

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/UberMorgott/morgue/internal/metadata"
	"github.com/UberMorgott/morgue/internal/odin"
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
		{Name: "Extract data layer", Required: false},
		{Name: "Decode Odin config", Required: false},
		{Name: "Extract strings", Required: false},
		{Name: "Build indexes", Required: false},
	}
}

func (i *IL2CPP) RequiredTools() []string {
	// il2cppinspector is the preferred dumper (its RuntimeDeps pull in the
	// .NET 10 ASP.NET runtime, auto-installed by the engine before Execute);
	// il2cppdumper is kept as the fallback. assetripper drives the config-only
	// data-layer export (ScriptableObjects/MonoBehaviour) that the Odin decode
	// stage then renders.
	return []string{"il2cppinspector", "il2cppdumper", "ilspycmd", "assetripper", "strings"}
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

	// Step 0: Copy originals. Always persisted (a single copy of the target +
	// metadata is cheap and valuable for reproducibility) — kept consistent
	// across recipes.
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
	if err := copyFile(metadataPath, filepath.Join(origDir, filepath.Base(metadataPath))); err != nil {
		report(0, Failed, time.Since(start), err, "")
		return err
	}
	log(fmt.Sprintf("Copied GameAssembly.dll and %s", filepath.Base(metadataPath)))
	report(0, Success, time.Since(start), nil, "")

	// Step 1: Extract metadata. Version-aware dumper selection with fallback:
	// detect the global-metadata.dat version, prefer Il2CppInspectorRedux when it
	// is installed AND supports that version, and fall back to the legacy
	// Il2CppDumper otherwise. Both tools populate the same DummyDll dir so the
	// downstream decompile/index steps are unchanged. If every dumper fails, the
	// error names the detected version + every tool tried + each tool error
	// (never a silent failure).
	report(1, Running, 0, nil, "")
	start = time.Now()

	// Structured output layout rooted at ctx.Output (the engine already hands us a
	// per-target dir, so the game segment is empty — no double nesting):
	//   dump/cs  il2cpp.cs       dump/dll  DummyDLLs
	//   data     config export   log       .tmp (spawn TEMP/cwd, kept off C:)
	layout := newIL2CPPLayout(ctx.Output, "")
	if err := layout.mkdirAll(); err != nil {
		report(1, Failed, time.Since(start), err, "")
		return err
	}
	// force re-runs every stage even when its output marker exists. Driven by the
	// global KeepIntermediates inverse is wrong; we gate purely on KeepIntermediates
	// being unrelated — resume is the default, so force stays false here. (A CLI
	// --force flag can flip this in a later milestone.)
	force := false

	// Legacy Il2CppDumper writes a fixed DummyDll/ subdir; root it under .tmp so its
	// raw output is transient, then runLegacyDump mirrors the DLLs into dump/dll.
	legacyMetaDir := filepath.Join(layout.TmpDir, "legacy-dump")
	dummyDllDir := layout.DllDir

	metaVersion, verErr := metadata.ReadVersion(metadataPath)
	if verErr != nil {
		// Non-fatal: an unreadable header just means we can't route on version;
		// dumperOrder still picks a tool based on InspectorRedux availability.
		logTool("il2cpp", fmt.Sprintf("Could not read metadata version (%v); proceeding with dumper auto-selection", verErr))
	} else {
		logTool("il2cpp", fmt.Sprintf("Detected IL2CPP metadata version %d", metaVersion))
	}

	csMarker := filepath.Join(layout.CsDir, "il2cpp.cs")
	var toolUsed string
	if stageDone(csMarker, force) && fileNonEmpty(dummyDllDir) {
		logTool("il2cpp", "Dump already present (dump/cs + dump/dll) — skipping extraction")
		toolUsed = "cached"
	} else {
		used, derr := runDumpStage(ctx, metadataPath, legacyMetaDir, dummyDllDir, metaVersion, logTool)
		if derr != nil {
			report(1, Failed, time.Since(start), derr, used)
			return derr
		}
		toolUsed = used
	}

	// Count outputs for logging
	dummyDlls := countFiles(dummyDllDir, ".dll")
	logTool(toolUsed, fmt.Sprintf("%s produced %d dummy assemblies", toolUsed, dummyDlls))
	reportCount(1, time.Since(start), toolUsed, dummyDlls, "assemblies")

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
		langVerArgs = []string{"--languageversion", ctx.Config.CSharpLanguageVersion}
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
		dBin, dRun := dotnetExec(ctx.Ctx, ilspyPath, decompArgs)
		res, runErr := util.RunCmd(ctx.Ctx, dBin, dRun, "")

		ec := -1
		if res != nil {
			ec = res.ExitCode
		}

		if runErr != nil || ec != 0 {
			// Retry without project mode
			os.RemoveAll(outDir)
			os.MkdirAll(outDir, 0755)
			retryArgs := append([]string{"-o", outDir, dll}, langVerArgs...)
			rBin, rRun := dotnetExec(ctx.Ctx, ilspyPath, retryArgs)
			res, runErr = util.RunCmd(ctx.Ctx, rBin, rRun, "")
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
				Step: 2, Total: total, Name: dllName,
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

	// Step 3: Extract data layer (AssetRipper config-only export). Non-fatal: the
	// IL2CPP code dump is the primary deliverable, so a missing tool / data dir or
	// an export failure logs + reports but never aborts the run.
	report(3, Running, 0, nil, "assetripper")
	start = time.Now()
	rippedMarker := filepath.Join(layout.DataDir, ".ripped")
	gameDataDir := findGameDataDir(ctx.Target)
	if ripperPath, rErr := ctx.Tools.Resolve("assetripper"); rErr != nil {
		logTool("assetripper", fmt.Sprintf("assetripper unavailable, skipping data layer: %v", rErr))
		report(3, Skipped, time.Since(start), nil, "assetripper")
	} else if gameDataDir == "" {
		logTool("assetripper", "could not locate *_Data dir, skipping data layer")
		report(3, Skipped, time.Since(start), nil, "assetripper")
	} else if stageDone(rippedMarker, force) {
		logTool("assetripper", "data layer already extracted — skipping")
		reportCount(3, time.Since(start), "assetripper", countAssetFiles(layout.DataDir), "assets")
	} else {
		full := ctx.Config.IL2CPPFullExport
		logTool("assetripper", fmt.Sprintf("Exporting %s (full=%v)", gameDataDir, full))
		rLastLog := time.Now().Add(-2 * time.Second)
		rerr := RunAssetRipperExport(ctx.Ctx, ripperPath, gameDataDir, layout.DataDir, layout.TmpDir, full, func(line string) {
			if time.Since(rLastLog) >= time.Second && strings.TrimSpace(line) != "" {
				logTool("assetripper", strings.TrimSpace(line))
				rLastLog = time.Now()
			}
		})
		if rerr != nil {
			logTool("assetripper", fmt.Sprintf("AssetRipper export failed (non-fatal): %v", rerr))
			report(3, Failed, time.Since(start), rerr, "assetripper")
		} else {
			os.WriteFile(rippedMarker, []byte("ok"), 0644)
			reportCount(3, time.Since(start), "assetripper", countAssetFiles(layout.DataDir), "assets")
		}
	}

	// Step 4: Decode Odin config from the exported .asset files into a readable
	// tree. Each blob is decoded under recover() so a single malformed asset is
	// logged and skipped — never aborts the run.
	report(4, Running, 0, nil, "odin")
	start = time.Now()
	assetFiles := collectAssetFiles(layout.DataDir)
	if len(assetFiles) == 0 {
		logTool("odin", "no .asset files to decode, skipping Odin decode")
		report(4, Skipped, time.Since(start), nil, "odin")
	} else {
		decoded := 0
		var outBuf strings.Builder
		rule := strings.Repeat("#", 60)
		for _, af := range assetFiles {
			text, n, derr := decodeAssetSafe(af)
			if derr != nil {
				logTool("odin", fmt.Sprintf("Odin decode skipped %s: %v", filepath.Base(af), derr))
				continue
			}
			outBuf.WriteString("\n" + rule + "\n")
			outBuf.WriteString(fmt.Sprintf("## %s  (%d bytes)\n", filepath.Base(af), n))
			outBuf.WriteString(rule + "\n")
			outBuf.WriteString(text)
			decoded++
		}
		os.WriteFile(filepath.Join(layout.DataDir, "odin-decoded.txt"), []byte(outBuf.String()), 0644)
		logTool("odin", fmt.Sprintf("Decoded %d Odin config files", decoded))
		reportCount(4, time.Since(start), "odin", decoded, "configs")
	}

	// Step 5: Extract strings from GameAssembly.dll
	report(5, Running, 0, nil, "strings")
	start = time.Now()
	stringsPath, err := ctx.Tools.Resolve("strings")
	if err != nil {
		logTool("strings", fmt.Sprintf("strings tool not available: %v", err))
		report(5, Skipped, time.Since(start), nil, "strings")
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
						Step: 5, Total: total, Name: steps[5].Name,
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
		reportCount(5, time.Since(start), "strings", strCount, "strings")
	}

	// Step 6: Build indexes
	report(6, Running, 0, nil, "")
	start = time.Now()
	logTool("ilspycmd", "Building indexes for decompiled output")
	if _, statErr := os.Stat(srcDir); statErr != nil {
		logTool("ilspycmd", "No source to index — ilspycmd produced no src/ output, skipping")
		report(6, Skipped, time.Since(start), nil, "")
	} else if idx, err := buildIndex(srcDir); err != nil {
		logTool("ilspycmd", fmt.Sprintf("Build indexes failed: %v", err))
		report(6, Failed, time.Since(start), err, "")
	} else {
		logTool("ilspycmd", fmt.Sprintf("Indexed %d source files (%d bytes) -> index.json", idx.FileCount, idx.TotalBytes))
		reportCount(6, time.Since(start), "", idx.FileCount, "files")
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

// findGameDataDir returns the Unity *_Data directory for a GameAssembly path.
// Three real layouts are covered, in order:
//  1. GameAssembly.dll lives INSIDE <Game>_Data        -> its own dir is the answer.
//  2. GameAssembly.dll lives at the game ROOT (Steam)  -> <Game>_Data is a CHILD.
//  3. fallback                                          -> a SIBLING *_Data dir.
//
// The Steam layout (case 2: GameAssembly.dll beside <Game>_Data, e.g.
// ".../Lost Castle 2/GameAssembly.dll" + ".../Lost Castle 2/LostCastle2_Data")
// is the common one and must be detected.
func findGameDataDir(target string) string {
	dir := filepath.Dir(target)

	// Case 1: the binary already sits inside a *_Data dir.
	if strings.HasSuffix(strings.ToLower(dir), "_data") {
		return dir
	}

	// Case 2: a *_Data dir is a direct child of the binary's dir (Steam root).
	if child := firstDataDir(dir); child != "" {
		return child
	}

	// Case 3: a *_Data dir is a sibling of the binary's dir.
	if sib := firstDataDir(filepath.Dir(dir)); sib != "" {
		return sib
	}
	return ""
}

// firstDataDir returns the first immediate child of parent whose name ends in
// "_data" (case-insensitive), or "" if none.
func firstDataDir(parent string) string {
	entries, _ := os.ReadDir(parent)
	for _, e := range entries {
		if e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), "_data") {
			return filepath.Join(parent, e.Name())
		}
	}
	return ""
}

// collectAssetFiles walks a directory tree for *.asset files (recursive).
func collectAssetFiles(root string) []string {
	var out []string
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".asset") {
			out = append(out, path)
		}
		return nil
	})
	return out
}

// countAssetFiles counts *.asset files under root (recursive).
func countAssetFiles(root string) int { return len(collectAssetFiles(root)) }

// fileNonEmpty reports whether a path exists and (for dirs) has entries.
func fileNonEmpty(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.IsDir() {
		entries, _ := os.ReadDir(path)
		return len(entries) > 0
	}
	return info.Size() > 0
}

// decodeAssetSafe wraps odin.DecodeAssetFile with a panic recovery so a single
// malformed Odin blob (the reader panics on a bad string length) is turned into
// an error and skipped rather than aborting the whole pipeline.
func decodeAssetSafe(path string) (text string, n int, err error) {
	defer func() {
		if r := recover(); r != nil {
			text, n = "", 0
			err = fmt.Errorf("panic decoding %s: %v", filepath.Base(path), r)
		}
	}()
	return odin.DecodeAssetFile(path)
}

// copyDirFlat copies all regular files from src into dst (non-recursive — enough
// for Il2CppDumper's flat DummyDll output).
func copyDirFlat(src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if err := copyFile(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return nil
}
