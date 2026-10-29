package recipe

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/UberMorgott/morgue/internal/gamedata"
	"github.com/UberMorgott/morgue/internal/quality"
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
		{Name: "Extract game assets (AssetRipper)", Required: false},
		{Name: "Organize game data", Required: false},
	}
}

func (u *UnityMono) RequiredTools() []string {
	// assetstudiomod (inventory) is intentionally NOT required: it is Optional
	// and the engine aborts the run if ANY RequiredTools entry stays missing
	// after install. The Organize step degrades gracefully without it.
	return []string{"ilspycmd", "strings", "assetripper"}
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
			_ = os.WriteFile(stringsOut, []byte(result.Stdout), 0644)
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
	_ = os.MkdirAll(srcDir, 0755)
	ilspyArgs := ilspyCommandArgs(ctx, ctx.Target, []string{"-p", "-o", srcDir})
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
		_ = os.RemoveAll(srcDir)
		_ = os.MkdirAll(srcDir, 0755)
		retryArgs := ilspyCommandArgs(ctx, ctx.Target, []string{"-o", srcDir})
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
			fallbackOK := perTypeFallback(ctx, ilspyPath, ctx.Target, srcDir, func(msg string) { logTool("ilspycmd", msg) })
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
	if ctx.Provenance != nil {
		module := filepath.Base(ctx.Target)
		inputSHA, herr := util.SHA256File(ctx.Target)
		if herr != nil {
			logTool("ilspycmd", fmt.Sprintf("provenance: hash %s: %v", module, herr))
		}
		for _, row := range []struct{ rel, kind, tool, step, detail string }{
			{"original/" + module, "original", "", "", "copied input assembly"},
			{"src", "decompiled-source", "ilspycmd", unityStepManagedDecomp, "ILSpy decompilation of managed IL; readable, not original source"},
		} {
			if perr := unityArtifactProv(ctx, inputSHA, row.rel, row.kind, module, row.tool, row.step, nil, row.detail); perr != nil {
				logTool("ilspycmd", fmt.Sprintf("provenance: %v", perr))
			}
		}
	}

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

	// Step 4: Extract game assets (AssetRipper full UnityProject export). Non-fatal:
	// the C# decompile is the primary deliverable, so a missing tool / data dir or
	// an export failure logs + reports but never aborts the run.
	report(4, Running, 0, nil, "assetripper")
	start = time.Now()
	// The AssetRipper export is per-GAME, not per-assembly: it dumps the whole
	// *_Data asset tree (10-20 GB on a real title). Writing it under ctx.Output
	// made every managed assembly re-export an identical copy (141 assemblies of
	// Valheim projected to ~2.5 TB). Keyed by the *_Data dir under the run-wide
	// root instead; later assemblies reuse only the current run's completed work.
	gameDataDir := monoGameDataDir(ctx.Target)
	assetRoot := sharedAssetDir(ctx, gameDataDir)
	rawExportDir := filepath.Join(assetRoot, "raw-export")
	tmpDir := filepath.Join(assetRoot, ".tmp")
	assetEv := newUnityDataEvidence(ctx, u.Name(), gameDataDir, rawExportDir, assetRoot != ctx.Output)
	assetEv.Mode = "unity-project"
	defer func() { writeUnityDataEvidence(ctx, &assetEv, logTool) }()
	if ctx.SkipAssets() {
		assetEv.State, assetEv.Reason = quality.UnityAssetsDisabled, "--code-only or UnityExtractAssets=false"
		logTool("assetripper", "asset extraction disabled (--code-only / UnityExtractAssets), skipping game-asset export")
		report(4, Skipped, time.Since(start), nil, "assetripper")
	} else if ripperPath, rErr := ctx.Tools.Resolve("assetripper"); rErr != nil {
		assetEv.State, assetEv.Reason = quality.UnityAssetsToolMissing, rErr.Error()
		logTool("assetripper", fmt.Sprintf("assetripper unavailable, skipping game-asset export: %v", rErr))
		report(4, Skipped, time.Since(start), nil, "assetripper")
	} else if gameDataDir == "" {
		assetEv.State, assetEv.Reason = quality.UnityAssetsNoDataDir, "no Unity *_Data directory two levels above "+filepath.Base(ctx.Target)
		logTool("assetripper", "could not locate Unity *_Data dir, skipping game-asset export")
		report(4, Skipped, time.Since(start), nil, "assetripper")
	} else {
		logTool("assetripper", fmt.Sprintf("Exporting %s (full=true)", gameDataDir))
		if ctx.Inputs != nil {
			if err := ctx.Inputs.Record("unity-data", gameDataDir); err != nil {
				return err
			}
		}
		rLastLog := time.Now().Add(-2 * time.Second)
		reused, rerr := runSharedProduct(ctx, sharedProductKey(ctx, gameDataDir, "mono-assets", "full=true", rawExportDir), rawExportDir, "asset", "extract-assets", "assetripper", func() error {
			return RunAssetRipperExport(ctx.Ctx, ripperPath, gameDataDir, rawExportDir, tmpDir, true, func(line string) {
				if time.Since(rLastLog) >= time.Second && strings.TrimSpace(line) != "" {
					logTool("assetripper", strings.TrimSpace(line))
					rLastLog = time.Now()
				}
			})
		})
		if rerr != nil {
			assetEv.State, assetEv.Reason = quality.UnityAssetsFailed, rerr.Error()
			logTool("assetripper", fmt.Sprintf("AssetRipper export failed (non-fatal): %v", rerr))
			report(4, Failed, time.Since(start), rerr, "assetripper")
		} else {
			assetEv.State = quality.UnityAssetsExported
			if reused {
				assetEv.State = quality.UnityAssetsReused
				logTool("assetripper", "reused completed export from this live run")
			}
			countUnityExport(&assetEv, rawExportDir)
			reportCount(4, time.Since(start), "assetripper", countAssetFiles(rawExportDir), "assets")
		}
	}

	// Step 5: Organize game data into a greppable text tree. Non-fatal: an organize
	// failure logs + reports but never aborts the run.
	report(5, Running, 0, nil, "gamedata")
	start = time.Now()
	exportAssets := filepath.Join(rawExportDir, "ExportedProject", "Assets")
	if _, statErr := os.Stat(exportAssets); statErr != nil {
		logTool("gamedata", "no AssetRipper export found, skipping game-data organize")
		assetEv.Organize = &quality.UnityStep{State: "skipped", Reason: "no AssetRipper export", Unit: "defs"}
		report(5, Skipped, time.Since(start), nil, "gamedata")
	} else {
		gameDataOut := ctx.GameDataOut
		if gameDataOut == "" {
			// Same per-game reasoning as the export: organizing a shared export
			// once per assembly would rebuild an identical tree N times.
			gameDataOut = filepath.Join(assetRoot, "GameData")
		}
		inventoryCSV := ""
		if cand := filepath.Join(ctx.Output, "inventory", "assets.csv"); fileNonEmpty(cand) {
			inventoryCSV = cand
		}
		toolVersions := map[string]string{}
		if st := ctx.Tools.Check("assetripper"); st.Version != "" {
			toolVersions["assetripper"] = st.Version
		}
		defTotal := 0
		reused, gerr := runSharedProduct(ctx, sharedProductKey(ctx, gameDataDir, "mono-organize", rawExportDir, gameDataOut), gameDataOut, "asset", "organize-data", "gamedata", func() error {
			rep, gerr := gamedata.Organize(gamedata.Options{
				ExportDir:     exportAssets,
				FullExportDir: exportAssets,
				OutDir:        gameDataOut,
				InventoryCSV:  inventoryCSV,
				ToolVersions:  toolVersions,
				Log:           func(s string) { logTool("gamedata", s) },
				Progress: func(d, t int, u string) {
					if ctx.Progress != nil {
						ctx.Progress <- StepProgress{
							Step: 5, Total: total, Name: steps[5].Name,
							Tool: "gamedata", Status: Running,
							Count: d, CountTotal: t, Unit: u,
						}
					}
				},
			})
			if gerr == nil {
				for _, c := range rep.DefCountsByType {
					defTotal += c
				}
			}
			return gerr
		})
		org := &quality.UnityStep{Output: unityRelSlash(ctx.Output, gameDataOut), External: ctx.GameDataOut != "", Unit: "defs"}
		if org.External {
			org.Output = filepath.ToSlash(gameDataOut)
		}
		assetEv.Organize = org
		if gerr != nil {
			org.State, org.Reason = "failed", gerr.Error()
			logTool("gamedata", fmt.Sprintf("Organize failed (non-fatal): %v", gerr))
			report(5, Failed, time.Since(start), gerr, "gamedata")
		} else {
			org.State = "done"
			if reused {
				org.State = "reused"
			} else {
				d := int64(defTotal)
				org.Count = &d
			}
			if reused {
				logTool("gamedata", "reused organized data from this live run")
			} else {
				logTool("gamedata", fmt.Sprintf("Organized %d defs", defTotal))
			}
			reportCount(5, time.Since(start), "gamedata", defTotal, "defs")
		}
	}

	return nil
}

// sharedAssetDir returns the run-wide directory holding the AssetRipper export
// for one game, e.g. <run root>/_assets/Valheim_Data-<identity>. Every target of
// the same game resolves to the same path; equal basenames from different games
// remain distinct. Falls back to the per-target output when there is no run
// root (direct recipe use) or no *_Data dir.
func sharedAssetDir(ctx *Context, gameDataDir string) string {
	root := ctx.SharedOut
	if root == "" {
		root = filepath.Dir(ctx.Output)
	}
	if root == "" || root == "." || gameDataDir == "" {
		return ctx.Output
	}
	// gameDataDir is an existing directory, so its base is already a legal segment.
	identity := canonicalGamePath(gameDataDir)
	hash := sha256.Sum256([]byte(identity))
	return filepath.Join(root, "_assets", filepath.Base(gameDataDir)+"-"+hex.EncodeToString(hash[:4]))
}

func canonicalGamePath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	return strings.ToLower(filepath.Clean(path))
}

func sharedProductKey(ctx *Context, gameDataDir, product, settings, output string) string {
	identity := canonicalGamePath(gameDataDir)
	if hash, err := util.SHA256File(filepath.Join(gameDataDir, "globalgamemanagers")); err == nil {
		identity += "|" + hash
	}
	version := ""
	if ctx.Tools != nil {
		version = ctx.Tools.Check("assetripper").Version
	}
	return product + "|" + identity + "|" + settings + "|" + version + "|" + canonicalGamePath(output)
}

func runSharedProduct(ctx *Context, key, output, kind, stepID, tool string, fn func() error) (bool, error) {
	work := func() error {
		if err := fn(); err != nil {
			return err
		}
		if !fileNonEmpty(output) {
			return fmt.Errorf("%s produced no output in %s", tool, output)
		}
		return nil
	}
	reused := false
	var err error
	if ctx.Shared != nil {
		reused, err = ctx.Shared.Do(key, work)
	} else {
		err = work()
	}
	if err != nil {
		return false, err
	}
	if !fileNonEmpty(output) {
		return false, fmt.Errorf("shared product disappeared: %s", output)
	}
	if ctx.Artifacts != nil {
		if err := ctx.Artifacts.Record(output, kind, stepID, tool, true); err != nil {
			return reused, err
		}
	}
	return reused, nil
}

// monoGameDataDir returns the Unity *_Data directory for a Mono target. For Mono
// builds Assembly-CSharp.dll lives at <Data>/Managed/, so the Data dir is two
// levels up. It is returned only when it looks like a real Unity data dir (name
// ends in "_Data" or it contains a globalgamemanagers file).
func monoGameDataDir(target string) string {
	dir := filepath.Dir(filepath.Dir(target))
	if dir == "" || dir == "." {
		return ""
	}
	if strings.HasSuffix(strings.ToLower(dir), "_data") {
		return dir
	}
	if _, err := os.Stat(filepath.Join(dir, "globalgamemanagers")); err == nil {
		return dir
	}
	return ""
}
