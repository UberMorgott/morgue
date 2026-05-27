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

	// Find game root — target might be an exe or directory
	gameRoot := ctx.Target
	if info, err := os.Stat(gameRoot); err == nil && !info.IsDir() {
		gameRoot = filepath.Dir(gameRoot)
	}

	pakFiles := findPakFiles(gameRoot)
	log(fmt.Sprintf("Game root: %s, found %d PAK/IoStore files", gameRoot, len(pakFiles)))

	// Step 0: Extract PAK assets
	if ctx.Config != nil && !ctx.Config.UE5ExtractPAK {
		report(0, Skipped, 0, nil)
		log("PAK extraction disabled in settings")
	} else {
		report(0, Running, 0, nil)
		start := time.Now()
		retocPath, err := ctx.Tools.Resolve("retoc")
		if err != nil {
			log(fmt.Sprintf("retoc not available: %v — skipping PAK extraction", err))
			report(0, Skipped, time.Since(start), nil)
		} else if len(pakFiles) == 0 {
			log("No .pak/.utoc files found — skipping extraction")
			report(0, Skipped, time.Since(start), nil)
		} else {
			extractDir := filepath.Join(ctx.Output, "extracted")
			os.MkdirAll(extractDir, 0755)
			failCount := 0
			for _, pak := range pakFiles {
				log(fmt.Sprintf("Extracting: %s", filepath.Base(pak)))
				result, runErr := util.RunCmd(ctx.Ctx, retocPath, []string{"extract", pak, "-o", extractDir}, "")
				if runErr != nil {
					log(fmt.Sprintf("retoc failed on %s: %v", filepath.Base(pak), runErr))
					failCount++
				} else if result != nil && result.ExitCode != 0 {
					log(fmt.Sprintf("retoc exit %d on %s: %s", result.ExitCode, filepath.Base(pak), result.Stderr))
					failCount++
				}
			}
			if failCount == len(pakFiles) {
				report(0, Failed, time.Since(start), fmt.Errorf("all %d PAK extractions failed", failCount))
			} else {
				report(0, Success, time.Since(start), nil)
			}
		}
	}

	// Step 1: SDK class dump (stub — requires runtime injection or static RTTI parse)
	if ctx.Config != nil && !ctx.Config.UE5SDKDump {
		report(1, Skipped, 0, nil)
		log("SDK class dump disabled in settings")
	} else {
		report(1, Running, 0, nil)
		start := time.Now()
		log("SDK class dump: not yet implemented (requires UE4SS runtime injection)")
		report(1, Skipped, time.Since(start), nil)
	}

	// Step 2: Extract strings
	if ctx.Config != nil && !ctx.Config.UE5ExtractStrings {
		report(2, Skipped, 0, nil)
		log("String extraction disabled in settings")
	} else {
		report(2, Running, 0, nil)
		start := time.Now()
		stringsPath, err := ctx.Tools.Resolve("strings")
		if err != nil {
			log(fmt.Sprintf("strings tool not available: %v", err))
			report(2, Skipped, time.Since(start), nil)
		} else {
			gameExe := findGameExe(gameRoot)
			if gameExe == "" {
				log("No game executable found for string extraction")
				report(2, Skipped, time.Since(start), nil)
			} else {
				stringsOut := filepath.Join(ctx.Output, "strings.txt")
				log(fmt.Sprintf("Extracting strings from: %s", filepath.Base(gameExe)))
				result, _ := util.RunCmd(ctx.Ctx, stringsPath, []string{"-nobanner", "-accepteula", gameExe}, "")
				if result != nil {
					os.WriteFile(stringsOut, []byte(result.Stdout), 0644)
					lines := strings.Count(result.Stdout, "\n")
					log(fmt.Sprintf("Extracted %d strings", lines))
				}
				report(2, Success, time.Since(start), nil)
			}
		}
	}

	// Step 3: Ghidra decompilation (optional, long-running)
	if ctx.Config != nil && !ctx.Config.UE5GhidraDecompile {
		report(3, Skipped, 0, nil)
		log("Ghidra decompilation disabled in settings")
	} else {
		report(3, Running, 0, nil)
		start := time.Now()
		log("Ghidra decompilation: stub (enable in settings for full binary analysis)")
		report(3, Skipped, time.Since(start), nil)
	}

	// Step 4: Name resolution (stub)
	if ctx.Config != nil && !ctx.Config.UE5NameResolution {
		report(4, Skipped, 0, nil)
		log("Name resolution disabled in settings")
	} else {
		report(4, Running, 0, nil)
		start := time.Now()
		log("Name resolution: stub (requires SDK dump output)")
		report(4, Skipped, time.Since(start), nil)
	}

	// Step 5: Build search indexes (stub)
	if ctx.Config != nil && !ctx.Config.UE5BuildIndexes {
		report(5, Skipped, 0, nil)
		log("Build indexes disabled in settings")
	} else {
		report(5, Running, 0, nil)
		start := time.Now()
		log("Build indexes: stub")
		report(5, Skipped, time.Since(start), nil)
	}

	// Step 6: Export hookable symbols (stub)
	if ctx.Config != nil && !ctx.Config.UE5ExportHookable {
		report(6, Skipped, 0, nil)
		log("Export hookable symbols disabled in settings")
	} else {
		report(6, Running, 0, nil)
		start := time.Now()
		log("Export hookable symbols: stub")
		report(6, Skipped, time.Since(start), nil)
	}

	return nil
}

// findPakFiles recursively finds .pak and .utoc files under root.
func findPakFiles(root string) []string {
	var paks []string
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
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

// findGameExe finds the main shipping executable in a UE game directory.
// Looks for the largest .exe that isn't a known engine tool.
func findGameExe(root string) string {
	skipNames := map[string]bool{
		"crashreportclient.exe":        true,
		"unrealcefsubprocess.exe":      true,
		"easyanticheat_eosservice.exe": true,
	}
	var best string
	var bestSize int64

	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".exe" {
			return nil
		}
		if skipNames[strings.ToLower(filepath.Base(path))] {
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
