package recipe

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/UberMorgott/morgue/internal/recon"
	"github.com/UberMorgott/morgue/internal/util"
)

// nameResolution is the structure written to <ctx.Output>/name_resolution.json
// by the UE5 name-resolution step (F5). It surfaces the offline-recoverable
// symbol stats (from F2's Ghidra symbols.json) plus .usmap detection. It does
// NOT claim full address->UObject resolution (which needs runtime data).
type nameResolution struct {
	SymbolsSource   string  `json:"symbols_source"`
	Named           int     `json:"named"`
	Total           int     `json:"total"`
	NamedPct        float64 `json:"named_pct"`
	ClassesCount    int     `json:"classes_count"`
	ResolvedOffline int     `json:"resolved_offline"` // FUN_ renamed from referenced symbol strings (B3)
	USmapFound      bool    `json:"usmap_found"`
	USmapPath       string  `json:"usmap_path"`
}

// findUsmap returns the path of the first .usmap mapping file found under any
// of the given roots (bounded WalkDir, like findPakFiles). Empty roots and
// missing dirs are skipped. Returns "" if none found.
func findUsmap(roots ...string) string {
	for _, root := range roots {
		if root == "" {
			continue
		}
		var found string
		filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if strings.ToLower(filepath.Ext(path)) == ".usmap" {
				found = path
				return filepath.SkipAll
			}
			return nil
		})
		if found != "" {
			return found
		}
	}
	return ""
}

// UE5 handles Unreal Engine 5 game analysis.
type UE5 struct{}

func init() {
	Register(&UE5{})
}

func (u *UE5) Name() string        { return "ue5" }
func (u *UE5) Description() string { return "Analyze Unreal Engine 5 game" }

func (u *UE5) Match(r *recon.Result) bool {
	return r.Kind == recon.UnrealEngine
}

func (u *UE5) Steps() []StepInfo {
	return []StepInfo{
		{Name: "Extract PAK assets", Required: false},
		{Name: "SDK class dump", Required: false},
		{Name: "Extract strings", Required: false},
		{Name: "Ghidra decompilation", Required: false},
		{Name: "Name resolution", Required: false},
		{Name: "Build search indexes", Required: false},
		{Name: "Export hookable symbols", Required: false},
	}
}

func (u *UE5) RequiredTools() []string {
	return []string{"retoc", "strings"}
}

func (u *UE5) Execute(ctx *Context) error {
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
	log := func(msg string) {
		if ctx.Log != nil {
			ctx.Log <- msg
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

	// Find game root — target might be an exe or directory
	gameRoot := ctx.Target
	if info, err := os.Stat(gameRoot); err == nil && !info.IsDir() {
		gameRoot = filepath.Dir(gameRoot)
	}

	pakFiles := findPakFiles(gameRoot)
	log(fmt.Sprintf("Game root: %s, found %d PAK/IoStore files", gameRoot, len(pakFiles)))

	// Step 0: Extract PAK assets
	if ctx.Config != nil && !ctx.Config.UE5ExtractPAK {
		report(0, Skipped, 0, nil, "retoc")
		log("PAK extraction disabled in settings")
	} else {
		report(0, Running, 0, nil, "retoc")
		start := time.Now()
		retocPath, err := ctx.Tools.Resolve("retoc")
		if err != nil {
			log(fmt.Sprintf("retoc not available: %v — skipping PAK extraction", err))
			report(0, Skipped, time.Since(start), nil, "retoc")
		} else {
			// retoc to-legacy operates on IoStore .utoc containers (Zen->Legacy
			// .uasset/.uexp). It must be pointed at the DIRECTORY containing the
			// .utoc files, not an individual .utoc: a per-container .utoc lacks the
			// base game's global ScriptObjects chunk (in global.utoc, which lives
			// in the same dir), so a directory input lets retoc resolve it.
			tocDirs := utocDirs(pakFiles)
			if len(tocDirs) == 0 {
				log("No .utoc IoStore containers found — skipping extraction (retoc to-legacy requires .utoc)")
				report(0, Skipped, time.Since(start), nil, "retoc")
			} else {
				// extractDir is always the recipe's own output subdir (never a
				// source dir), so clearing it can't touch the game files. Clear it
				// up front so stale assets from a prior run can't fake a success.
				extractDir := filepath.Join(ctx.Output, "extracted")
				os.RemoveAll(extractDir)
				os.MkdirAll(extractDir, 0755)

				okDirs := 0
				failedDirs := 0
				producedThisRun := 0 // counted per-dir for THIS run only
				cancelled := false
				var lastErr string
				multi := len(tocDirs) > 1
				for i, dir := range tocDirs {
					if ctx.Ctx != nil && ctx.Ctx.Err() != nil {
						cancelled = true
						break
					}
					// When multiple source dirs exist, give each its own subdir so
					// assets from different paks don't collide.
					outDir := extractDir
					if multi {
						outDir = filepath.Join(extractDir, fmt.Sprintf("%02d_%s", i, filepath.Base(dir)))
						os.MkdirAll(outDir, 0755)
					}
					log(fmt.Sprintf("Converting (to-legacy): %s", dir))
					result, runErr := util.RunCmdStreaming(ctx.Ctx, retocPath,
						[]string{"to-legacy", dir, outDir}, "",
						func(line string) { log("[retoc] " + line) })
					// Count only what THIS dir produced into its own outDir.
					dirProduced := countFilesRecursive(outDir)
					producedThisRun += dirProduced
					if multi && dirProduced == 0 {
						log(fmt.Sprintf("no assets extracted from %s (empty/patch container) — removing empty root", dir))
						os.RemoveAll(outDir)
					}
					switch {
					case runErr != nil:
						lastErr = runErr.Error()
						log(fmt.Sprintf("retoc failed on %s: %v", dir, runErr))
						failedDirs++
					case result != nil && result.ExitCode != 0:
						lastErr = strings.TrimSpace(result.Stderr)
						log(fmt.Sprintf("retoc exit %d on %s: %s", result.ExitCode, dir, lastErr))
						// A non-zero exit that still produced files (e.g. retoc
						// panics on one asset after extracting others) counts as a
						// per-dir failure, but the produced files are kept below.
						failedDirs++
					default:
						okDirs++
					}
				}
				switch {
				case cancelled:
					report(0, Failed, time.Since(start), ctx.Ctx.Err(), "retoc")
					log("PAK extraction cancelled")
				case failedDirs == 0:
					// All directories converted cleanly.
					log(fmt.Sprintf("Converted %d dir(s), %d asset file(s) produced", okDirs, producedThisRun))
					reportCount(0, time.Since(start), "retoc", producedThisRun, "files")
				case producedThisRun > 0:
					// Partial success: some dir(s) errored/panicked but files were
					// still produced this run. Keep them; surface the failure count.
					log(fmt.Sprintf("retoc: %d of %d dir(s) failed but %d asset file(s) were produced — keeping them. Last error: %s",
						failedDirs, len(tocDirs), producedThisRun, lastErr))
					reportCount(0, time.Since(start), "retoc", producedThisRun, "files")
				default:
					report(0, Failed, time.Since(start),
						fmt.Errorf("retoc to-legacy failed, 0 files produced (%d of %d dir(s) failed): %s",
							failedDirs, len(tocDirs), lastErr), "retoc")
				}
			}
		}
	}

	// Step 1: SDK class dump (stub — requires runtime injection or static RTTI parse)
	if ctx.Config != nil && !ctx.Config.UE5SDKDump {
		report(1, Skipped, 0, nil, "")
		log("SDK class dump disabled in settings")
	} else {
		report(1, Running, 0, nil, "")
		start := time.Now()
		log("SDK class dump: not yet implemented (requires UE4SS runtime injection)")
		report(1, Skipped, time.Since(start), nil, "")
	}

	// Step 2: Extract strings
	if ctx.Config != nil && !ctx.Config.UE5ExtractStrings {
		report(2, Skipped, 0, nil, "strings")
		log("String extraction disabled in settings")
	} else {
		report(2, Running, 0, nil, "strings")
		start := time.Now()
		stringsPath, err := ctx.Tools.Resolve("strings")
		if err != nil {
			log(fmt.Sprintf("strings tool not available: %v", err))
			report(2, Skipped, time.Since(start), nil, "strings")
		} else {
			gameExe := findGameExe(gameRoot)
			if gameExe == "" {
				log("No game executable found for string extraction")
				report(2, Skipped, time.Since(start), nil, "strings")
			} else {
				stringsOut := filepath.Join(ctx.Output, "strings.txt")
				log(fmt.Sprintf("Extracting strings from: %s", filepath.Base(gameExe)))
				result, _ := util.RunCmd(ctx.Ctx, stringsPath, []string{"-nobanner", "-accepteula", gameExe}, "")
				if result != nil {
					os.WriteFile(stringsOut, []byte(result.Stdout), 0644)
					lines := strings.Count(result.Stdout, "\n")
					log(fmt.Sprintf("Extracted %d strings", lines))
				}
				// Analyze and structure strings
				analyzeStrings(stringsOut, filepath.Join(ctx.Output, "strings.json"))
				strCount := countLines(stringsOut)
				reportCount(2, time.Since(start), "strings", strCount, "strings")
			}
		}
	}

	// Step 3: Ghidra decompilation (optional, long-running)
	if ctx.Config != nil && !ctx.Config.UE5GhidraDecompile {
		report(3, Skipped, 0, nil, "ghidra")
		log("Ghidra decompilation disabled in settings")
	} else {
		report(3, Running, 0, nil, "ghidra")
		start := time.Now()
		ghidraPath, err := ctx.Tools.Resolve("ghidra")
		if err != nil {
			log(fmt.Sprintf("Ghidra not available: %v — skipping decompilation", err))
			report(3, Skipped, time.Since(start), nil, "ghidra")
		} else {
			nativeBin := findUEShippingExe(gameRoot)
			if nativeBin == "" {
				log("No UE shipping executable found under Binaries/Win64 — skipping Ghidra decompilation")
				report(3, Skipped, time.Since(start), nil, "ghidra")
			} else {
				log(fmt.Sprintf("Ghidra decompiling: %s", filepath.Base(nativeBin)))
				srcDir := filepath.Join(ctx.Output, "src")
				funcCount, runErr := runGhidra(ctx.Ctx, ghidraPath, resolveGhidraJava(ctx.Tools), nativeBin, srcDir,
					func(msg string) { log("[ghidra] " + msg) },
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
					log(fmt.Sprintf("Ghidra decompilation failed: %v", runErr))
					report(3, Failed, time.Since(start), runErr, "ghidra")
				} else {
					reportCount(3, time.Since(start), "ghidra", funcCount, "functions")
					// Split combined .c into per-function files + symbols.json
					// (F1/F2). Guarded by the 'combined .c exists' check inside
					// the helper; logged-not-fatal.
					if res, splitErr := splitAndIndexDecompiledC(srcDir, nativeBin); splitErr != nil {
						log(fmt.Sprintf("[ghidra] Function split failed (combined .c kept): %v", splitErr))
					} else if res != nil {
						log(fmt.Sprintf("[ghidra] Split %d functions (%d named, %.1f%%) -> functions/ + symbols.json",
							res.FunctionCount, res.NamedCount, res.NamedPct))
					}
				}
			}
		}
	}

	// Step 4: Name resolution (stub)
	if ctx.Config != nil && !ctx.Config.UE5NameResolution {
		report(4, Skipped, 0, nil, "")
		log("Name resolution disabled in settings")
	} else {
		report(4, Running, 0, nil, "")
		start := time.Now()
		// Offline name resolution (F5). True address->UObject-method resolution
		// needs runtime SDK/RTTI data we do NOT produce offline. What we CAN do
		// offline: surface the demangled symbol stats produced by F2's Ghidra
		// pass (symbols.json) and detect any .usmap mapping file for future use.
		srcDir := filepath.Join(ctx.Output, "src")
		symbolsPath := filepath.Join(srcDir, "symbols.json")
		usmapPath := findUsmap(gameRoot, filepath.Join(ctx.Output, "extracted"))

		if !fileExists(symbolsPath) && usmapPath == "" {
			log("Name resolution: no symbols.json and no .usmap found — skipping " +
				"(full address->UObject resolution requires runtime data unavailable offline)")
			report(4, Skipped, time.Since(start), nil, "")
		} else {
			nr := nameResolution{
				USmapFound: usmapPath != "",
				USmapPath:  filepath.ToSlash(usmapPath),
			}
			named := 0
			// Offline string->symbol resolution (B3): conservatively rename
			// anonymous FUN_ functions from the distinctive C++ identifiers they
			// reference, and clean intrinsic noise from the call graph. Streaming
			// + memory-safe; failure is logged-not-fatal so the step never
			// regresses the working counts path.
			if fileExists(symbolsPath) {
				if st, rerr := resolveNames(srcDir); rerr != nil {
					log(fmt.Sprintf("Offline name resolution skipped (non-fatal): %v", rerr))
				} else if st.Resolved > 0 {
					nr.ResolvedOffline = st.Resolved
					log(fmt.Sprintf("Resolved %d anonymous functions from referenced symbol strings", st.Resolved))
				}
			}
			if fileExists(symbolsPath) {
				if data, rerr := os.ReadFile(symbolsPath); rerr == nil {
					var sm symbolMap
					if json.Unmarshal(data, &sm) == nil {
						nr.SymbolsSource = "ghidra"
						nr.Named = sm.Counts.Named
						nr.Total = sm.Counts.Total
						nr.NamedPct = sm.Counts.NamedPct
						nr.ClassesCount = len(sm.Classes)
						named = sm.Counts.Named
						log(fmt.Sprintf("Applied %d demangled symbols (named_pct=%.1f%%, %d classes) from Ghidra",
							sm.Counts.Named, sm.Counts.NamedPct, len(sm.Classes)))
					}
				}
			}
			if usmapPath != "" {
				log(fmt.Sprintf("Found .usmap mapping file: %s (full parse out of scope for v1, recorded only)", usmapPath))
			}
			log("Note: address->UObject / runtime-vtable resolution requires runtime (UE4SS) data — skipped")
			if data, merr := json.MarshalIndent(&nr, "", "  "); merr == nil {
				os.WriteFile(filepath.Join(ctx.Output, "name_resolution.json"), data, 0644)
			}
			reportCount(4, time.Since(start), "", named, "symbols")
		}
	}

	// Step 5: Build search indexes
	if ctx.Config != nil && !ctx.Config.UE5BuildIndexes {
		report(5, Skipped, 0, nil, "")
		log("Build indexes disabled in settings")
	} else {
		report(5, Running, 0, nil, "")
		start := time.Now()
		srcDir := filepath.Join(ctx.Output, "src")
		extractedDir := filepath.Join(ctx.Output, "extracted")
		_, srcErr := os.Stat(srcDir)
		_, extErr := os.Stat(extractedDir)
		hasSrc := srcErr == nil
		hasExtracted := extErr == nil
		if !hasSrc && !hasExtracted {
			log("Nothing to index — no src/ decompilation and no extracted/ assets, skipping")
			report(5, Skipped, time.Since(start), nil, "")
		} else {
			// Index both decompiled source and extracted game assets so an AI
			// consumer can navigate paks-only UE targets (empty/absent src/).
			// index.json is written to ctx.Output (alongside src/ and extracted/).
			indexSrc := ""
			if hasSrc {
				indexSrc = srcDir
			}
			indexExtracted := ""
			if hasExtracted {
				indexExtracted = extractedDir
			}
			if idx, err := buildUEIndex(ctx.Output, indexSrc, indexExtracted); err != nil {
				log(fmt.Sprintf("Build indexes failed: %v", err))
				report(5, Failed, time.Since(start), err, "")
			} else {
				log(fmt.Sprintf("Indexed %d file(s) (%d bytes) [src+assets] -> index.json", idx.FileCount, idx.TotalBytes))
				// Engine-boilerplate cleanup (B4): tag recovered classes as engine
				// boilerplate vs game-specific so navigation foregrounds game logic.
				// Nothing is deleted — every class is recorded with a flag.
				// Logged-not-fatal so the index build never regresses.
				if hasSrc {
					if total, b, cerr := writeClassClassification(indexSrc); cerr != nil {
						log(fmt.Sprintf("Class classification skipped (non-fatal): %v", cerr))
					} else if total > 0 {
						log(fmt.Sprintf("Classified %d classes (%d boilerplate, %d game) -> indexes/classes.json",
							total, b, total-b))
					}
				}
				reportCount(5, time.Since(start), "", idx.FileCount, "files")
			}
		}
	}

	// Step 6: Export hookable symbols (B4)
	if ctx.Config != nil && !ctx.Config.UE5ExportHookable {
		report(6, Skipped, 0, nil, "")
		log("Export hookable symbols disabled in settings")
	} else {
		report(6, Running, 0, nil, "")
		start := time.Now()
		srcDir := filepath.Join(ctx.Output, "src")
		if !fileExists(filepath.Join(srcDir, "functions.ndjson")) {
			log("Export hookable symbols: no functions.ndjson (Ghidra split not run) — skipping")
			report(6, Skipped, time.Since(start), nil, "")
		} else if n, herr := writeHookable(srcDir); herr != nil {
			log(fmt.Sprintf("Export hookable symbols failed: %v", herr))
			report(6, Failed, time.Since(start), herr, "")
		} else {
			log(fmt.Sprintf("Exported %d hookable functions (named/resolved, addr+name+signature) -> indexes/hookable.json", n))
			reportCount(6, time.Since(start), "", n, "functions")
		}
	}

	return nil
}

// utocDirs returns the unique parent directories that contain at least one
// .utoc IoStore container, derived from the discovered pak/IoStore files.
// retoc to-legacy must be pointed at the directory (not an individual .utoc)
// so it can resolve the global ScriptObjects container (global.utoc) that
// lives alongside the per-mod/per-chunk .utoc files. The result is
// de-duplicated and order-stable.
func utocDirs(files []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		var toc string
		switch strings.ToLower(filepath.Ext(f)) {
		case ".utoc":
			toc = f
		case ".pak":
			// A .pak may have a sibling .utoc with the same basename.
			cand := f[:len(f)-len(filepath.Ext(f))] + ".utoc"
			if _, err := os.Stat(cand); err == nil {
				toc = cand
			}
		}
		if toc == "" {
			continue
		}
		dir := filepath.Dir(toc)
		if !seen[dir] {
			seen[dir] = true
			out = append(out, dir)
		}
	}
	return out
}

// countFilesRecursive counts regular files under root (0 if root is absent).
func countFilesRecursive(root string) int {
	n := 0
	filepath.WalkDir(root, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

// findPakFiles recursively finds .pak and .utoc files under root.
func findPakFiles(root string) []string {
	var paks []string
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".pak" || ext == ".utoc" {
			paks = append(paks, path)
		}
		return nil
	})
	return paks
}

// engineToolExes are known UE engine tool executables that should never be
// treated as the game binary (shipped under Engine/Binaries/Win64 etc.).
var engineToolExes = map[string]bool{
	"crashreportclient.exe":        true,
	"unrealcefsubprocess.exe":      true,
	"easyanticheat_eosservice.exe": true,
}

// findUEShippingExe locates the native game binary in a UE game directory.
// Preference order:
//  1. <root>/**/Binaries/Win64/*-Shipping.exe
//  2. largest non-tool .exe under a Binaries/Win64 directory
//  3. fall back to findGameExe (largest non-tool .exe anywhere under root)
func findUEShippingExe(root string) string {
	var shipping, bestWin64 string
	var bestWin64Size int64
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".exe" {
			return nil
		}
		dir := filepath.ToSlash(strings.ToLower(filepath.Dir(path)))
		if !strings.HasSuffix(dir, "binaries/win64") {
			return nil
		}
		base := strings.ToLower(filepath.Base(path))
		if engineToolExes[base] {
			return nil
		}
		if strings.HasSuffix(base, "-shipping.exe") {
			if shipping == "" {
				shipping = path
			}
		} else {
			info, infoErr := d.Info()
			if infoErr != nil {
				return nil
			}
			if info.Size() > bestWin64Size {
				bestWin64Size = info.Size()
				bestWin64 = path
			}
		}
		return nil
	})
	if shipping != "" {
		return shipping
	}
	if bestWin64 != "" {
		return bestWin64
	}
	return findGameExe(root)
}

// findGameExe finds the main shipping executable in a UE game directory.
// Looks for the largest .exe that isn't a known engine tool.
func findGameExe(root string) string {
	var best string
	var bestSize int64

	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".exe" {
			return nil
		}
		if engineToolExes[strings.ToLower(filepath.Base(path))] {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.Size() > bestSize {
			bestSize = info.Size()
			best = path
		}
		return nil
	})
	return best
}
