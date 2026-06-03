package recipe

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/UberMorgott/morgue/internal/recon"
	"github.com/UberMorgott/morgue/internal/util"
)

// Native is a catch-all recipe for native binaries.
type Native struct{}

func init() {
	Register(&Native{})
}

func (n *Native) Name() string        { return "native" }
func (n *Native) Description() string { return "Reverse-engineer native binary" }

func (n *Native) Match(r *recon.Result) bool {
	return r.Kind == recon.Native
}

func (n *Native) Steps() []StepInfo {
	return []StepInfo{
		{Name: "Copy original", Required: false},
		{Name: "Extract strings", Required: false},
		{Name: "Decompile with Ghidra", Required: true},
		{Name: "Build indexes", Required: false},
	}
}

func (n *Native) RequiredTools() []string {
	return []string{"ghidra"}
}

func (n *Native) Execute(ctx *Context) error {
	steps := n.Steps()
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

	// Batch mode: StepFilter limits which steps run.
	// "strings" = only copy + strings, "ghidra" = only ghidra, "" = all.
	runStrings := ctx.StepFilter == "" || ctx.StepFilter == "strings"
	doGhidra := ctx.StepFilter == "" || ctx.StepFilter == "ghidra"

	// Step 0: Copy original. Always persisted (a single copy of the target is
	// cheap and valuable for reproducibility) — kept consistent across all
	// recipes. Runs in the strings phase under batch mode.
	var start time.Time
	if runStrings {
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
	}

	// Step 1: Extract strings
	if runStrings {
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
			r, _ := util.RunCmdStreaming(ctx.Ctx, stringsPath, []string{"-nobanner", "-accepteula", ctx.Target}, "", func(line string) {
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
			if r != nil {
				os.WriteFile(stringsOut, []byte(r.Stdout), 0644)
			}
			// Analyze and structure strings
			analyzeStrings(stringsOut, filepath.Join(ctx.Output, "strings.json"))
			strCount := countLines(stringsOut)
			reportCount(1, time.Since(start), "strings", strCount, "strings")
		}
		// In strings-only mode, stop here
		if !doGhidra {
			return nil
		}
	}

	// Step 2: Decompile with Ghidra
	if doGhidra && ctx.Config != nil && !ctx.Config.NativeGhidraDecompile {
		report(2, Skipped, 0, nil, "ghidra")
		logTool("ghidra", "Ghidra decompilation disabled in settings")
	} else if doGhidra {
		report(2, Running, 0, nil, "ghidra")
		start = time.Now()
		ghidraPath, err := ctx.Tools.Resolve("ghidra")
		if err != nil {
			report(2, Failed, time.Since(start), fmt.Errorf("ghidra not available: %w", err), "ghidra")
			return fmt.Errorf("ghidra not available: %w", err)
		}

		srcDir := filepath.Join(ctx.Output, "src")
		funcCount, err := runGhidra(ctx.Ctx, ghidraPath, resolveGhidraJava(ctx.Tools), ctx.Target, srcDir,
			func(msg string) { logTool("ghidra", msg) },
			func(name string, count int) {
				if ctx.Progress != nil {
					ctx.Progress <- StepProgress{
						Step: 2, Total: total, Name: name,
						Tool: "ghidra", Status: Running,
						Count: count, Unit: "functions",
					}
				}
			},
		)
		if err != nil {
			report(2, Failed, time.Since(start), err, "ghidra")
			return err
		}
		reportCount(2, time.Since(start), "ghidra", funcCount, "functions")

		// Split the combined .c into per-function files + emit functions_index.json
		// + symbols.json (F1/F2). Additive, streaming; failure is logged-not-fatal
		// so the working decompile path never regresses.
		if res, splitErr := splitAndIndexDecompiledC(srcDir, ctx.Target); splitErr != nil {
			logTool("ghidra", fmt.Sprintf("Function split failed (combined .c kept): %v", splitErr))
		} else if res != nil {
			logTool("ghidra", fmt.Sprintf("Split %d functions (%d named, %.1f%%) -> functions/ + symbols.json",
				res.FunctionCount, res.NamedCount, res.NamedPct))
			reportCount(2, time.Since(start), "ghidra", res.FunctionCount, "functions")
		}
	}

	// Step 3: Build indexes
	report(3, Running, 0, nil, "")
	start = time.Now()
	logTool("ghidra", "Building indexes for decompiled output")
	srcDir := filepath.Join(ctx.Output, "src")
	if _, statErr := os.Stat(srcDir); statErr != nil {
		logTool("ghidra", "No source to index — Ghidra produced no src/ output, skipping")
		report(3, Skipped, time.Since(start), nil, "")
	} else if idx, err := buildIndex(srcDir); err != nil {
		logTool("ghidra", fmt.Sprintf("Build indexes failed: %v", err))
		report(3, Failed, time.Since(start), err, "")
	} else {
		logTool("ghidra", fmt.Sprintf("Indexed %d source files (%d bytes) -> index.json", idx.FileCount, idx.TotalBytes))
		// Phase B enrichment (parity with the UE5 recipe): offline name
		// resolution + engine-class flagging + hookable export over the streamed
		// indexes the split produced. All additive, streaming, logged-not-fatal
		// so the working native decompile path never regresses.
		if st, rerr := resolveNames(srcDir); rerr != nil {
			logTool("ghidra", fmt.Sprintf("Name resolution skipped (non-fatal): %v", rerr))
		} else if st.Resolved > 0 {
			logTool("ghidra", fmt.Sprintf("Resolved %d anonymous functions from referenced symbol strings", st.Resolved))
		}
		if total, b, cerr := writeClassClassification(srcDir); cerr != nil {
			logTool("ghidra", fmt.Sprintf("Class classification skipped (non-fatal): %v", cerr))
		} else if total > 0 {
			logTool("ghidra", fmt.Sprintf("Classified %d classes (%d boilerplate, %d game) -> indexes/classes.json",
				total, b, total-b))
		}
		if n, herr := writeHookable(srcDir); herr != nil {
			logTool("ghidra", fmt.Sprintf("Export hookable symbols skipped (non-fatal): %v", herr))
		} else if n > 0 {
			logTool("ghidra", fmt.Sprintf("Exported %d hookable functions -> indexes/hookable.json", n))
		}
		// AI-readability cleanup (U4) — parity with the UE5 recipe: collapse
		// templated function clones + emit game-only views. Additive, logged.
		runDedupeAndGameViews(srcDir, func(m string) { logTool("ghidra", m) })
		reportCount(3, time.Since(start), "", idx.FileCount, "files")
	}

	return nil
}
