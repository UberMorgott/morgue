package recipe

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	}
}

func (n *Native) RequiredTools() []string {
	return []string{"ghidra"}
}

// ghidraExportScript is a Java GhidraScript that decompiles all functions to C.
// Ghidra 12+ removed Jython; .py scripts require PyGhidra (CPython+JPype).
// Java scripts always work with analyzeHeadless out of the box.
const ghidraExportScript = `// MorgueExport.java — Ghidra headless decompile-to-C script
// Usage: analyzeHeadless ... -postScript MorgueExport.java <output_file>
import ghidra.app.script.GhidraScript;
import ghidra.app.decompiler.DecompInterface;
import ghidra.app.decompiler.DecompileResults;
import ghidra.app.decompiler.DecompiledFunction;
import ghidra.program.model.listing.Function;
import ghidra.program.model.listing.FunctionIterator;
import ghidra.util.task.ConsoleTaskMonitor;
import java.io.FileWriter;
import java.io.PrintWriter;

public class MorgueExport extends GhidraScript {
    @Override
    public void run() throws Exception {
        String[] args = getScriptArgs();
        String outputPath = args[0];

        DecompInterface decomp = new DecompInterface();
        decomp.openProgram(currentProgram);

        FunctionIterator funcs = currentProgram.getListing().getFunctions(true);

        PrintWriter f = new PrintWriter(new FileWriter(outputPath));
        f.println("// Decompiled by Ghidra via Morgue");
        f.println("// Binary: " + currentProgram.getExecutablePath());
        f.println("// Architecture: " + currentProgram.getLanguage());
        f.println();

        int count = 0;
        int errors = 0;
        while (funcs.hasNext()) {
            Function func = funcs.next();
            try {
                DecompileResults results = decomp.decompileFunction(func, 120, monitor);
                if (results != null && results.decompileCompleted()) {
                    DecompiledFunction c = results.getDecompiledFunction();
                    if (c != null) {
                        String sig = c.getSignature();
                        f.println("// " + func.getEntryPoint());
                        if (sig != null) {
                            f.println(sig);
                        }
                        f.println(c.getC());
                        f.println();
                        count++;
                        System.out.println("Morgue:fn:" + count + ":" + func.getName());
                        System.out.flush();
                    }
                } else {
                    f.println("// FAILED: " + func.getName() + " @ " + func.getEntryPoint());
                    f.println();
                    errors++;
                }
            } catch (Exception e) {
                f.println("// ERROR: " + func.getName() + " @ " + func.getEntryPoint() + " — " + e.getMessage());
                f.println();
                errors++;
            }
        }

        f.println();
        f.println("// Total: " + count + " functions decompiled, " + errors + " errors");
        f.close();
        decomp.dispose();

        System.out.println("Morgue: Decompiled " + count + " functions, " + errors + " errors -> " + outputPath);
        System.out.flush();
    }
}
`

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
	runGhidra := ctx.StepFilter == "" || ctx.StepFilter == "ghidra"

	// Step 0: Copy original (only when keeping intermediates)
	var start time.Time
	if runStrings {
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
			report(0, Success, time.Since(start), nil, "")
		} else {
			report(0, Skipped, 0, nil, "")
		}
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
		if !runGhidra {
			return nil
		}
	}

	// Step 2: Decompile with Ghidra
	if runGhidra {
		report(2, Running, 0, nil, "ghidra")
		start = time.Now()
		ghidraPath, err := ctx.Tools.Resolve("ghidra")
		if err != nil {
			report(2, Failed, time.Since(start), fmt.Errorf("ghidra not available: %w", err), "ghidra")
			return fmt.Errorf("ghidra not available: %w", err)
		}

		// ghidraPath points to ghidraRun.bat; we need support/analyzeHeadless
		ghidraDir := filepath.Dir(ghidraPath)
		analyzeHeadless := filepath.Join(ghidraDir, "support", "analyzeHeadless")
		if runtime.GOOS == "windows" {
			analyzeHeadless += ".bat"
		}

		// Create temp dir for Ghidra project files
		projDir, err := os.MkdirTemp("", "morgue-ghidra-*")
		if err != nil {
			report(2, Failed, time.Since(start), err, "ghidra")
			return err
		}
		defer os.RemoveAll(projDir)

		// Write the export script to a temp file.
		// The filename MUST be MorgueExport.java — Ghidra requires the filename
		// to match the public class name inside the script.
		scriptDir, err := os.MkdirTemp("", "morgue-ghidra-script-*")
		if err != nil {
			report(2, Failed, time.Since(start), err, "ghidra")
			return err
		}
		defer os.RemoveAll(scriptDir)
		scriptPath := filepath.Join(scriptDir, "MorgueExport.java")
		scriptFile, err := os.Create(scriptPath)
		if err != nil {
			report(2, Failed, time.Since(start), err, "ghidra")
			return err
		}

		if _, err = scriptFile.WriteString(ghidraExportScript); err != nil {
			scriptFile.Close()
			report(2, Failed, time.Since(start), err, "ghidra")
			return err
		}
		scriptFile.Close()

		// Prepare output directory and file
		srcDir := filepath.Join(ctx.Output, "src")
		if err := os.MkdirAll(srcDir, 0755); err != nil {
			report(2, Failed, time.Since(start), err, "ghidra")
			return err
		}

		baseName := strings.TrimSuffix(filepath.Base(ctx.Target), filepath.Ext(ctx.Target))
		outputFile := filepath.Join(srcDir, baseName+".c")

		// Run Ghidra analyzeHeadless with streaming for real-time progress
		logTool("ghidra", fmt.Sprintf("Running Ghidra analyzeHeadless on %s", filepath.Base(ctx.Target)))
		ghidraFuncCount := 0
		ghidraPhase := "analyzing"
		lastLogTime := time.Now().Add(-2 * time.Second) // allow first log immediately
		result, err := util.RunCmdStreaming(ctx.Ctx, analyzeHeadless, []string{
			projDir, "MorgueProject",
			"-import", ctx.Target,
			"-postScript", scriptPath, outputFile,
			"-scriptPath", filepath.Dir(scriptPath),
			"-deleteProject",
		}, "", func(line string) {
			// Parse "Morgue:fn:<count>:<funcName>" from our export script
			if strings.HasPrefix(line, "Morgue:fn:") {
				parts := strings.SplitN(line, ":", 4)
				if len(parts) >= 4 {
					fmt.Sscanf(parts[2], "%d", &ghidraFuncCount)
					if time.Since(lastLogTime) >= time.Second {
						logTool("ghidra", fmt.Sprintf("Decompiled %d functions (%s)", ghidraFuncCount, parts[3]))
						if ctx.Progress != nil {
							ctx.Progress <- StepProgress{
								Step: 2, Total: total, Name: steps[2].Name,
								Tool: "ghidra", Status: Running,
								Count: ghidraFuncCount, Unit: "functions",
							}
						}
						lastLogTime = time.Now()
					}
				}
			} else if ghidraFuncCount == 0 && len(strings.TrimSpace(line)) > 0 {
				// During analysis phase — detect Ghidra sub-phases and show progress
				lower := strings.ToLower(line)
				if strings.Contains(lower, "importing") {
					ghidraPhase = "importing"
				} else if strings.Contains(lower, "analyz") {
					ghidraPhase = "analyzing"
				} else if strings.Contains(lower, "disassembl") {
					ghidraPhase = "disassembling"
				}
				if time.Since(lastLogTime) >= time.Second {
					logTool("ghidra", fmt.Sprintf("[%s] %s", ghidraPhase, strings.TrimSpace(line)))
					if ctx.Progress != nil {
						ctx.Progress <- StepProgress{
							Step: 2, Total: total, Name: steps[2].Name,
							Tool: "ghidra", Status: Running,
							Count: 0, Unit: ghidraPhase,
						}
					}
					lastLogTime = time.Now()
				}
			}
		})

		exitCode := -1
		if result != nil {
			exitCode = result.ExitCode
		}
		if err != nil || exitCode != 0 {
			stderr := ""
			if result != nil && result.Stderr != "" {
				stderr = result.Stderr
			}
			execErr := fmt.Errorf("ghidra analyzeHeadless failed (exit %d): %s", exitCode, stderr)
			logTool("ghidra", execErr.Error())
			report(2, Failed, time.Since(start), execErr, "ghidra")
			return execErr
		}

		// Verify output file exists and is non-empty
		info, err := os.Stat(outputFile)
		if err != nil || info.Size() == 0 {
			execErr := fmt.Errorf("ghidra produced no output at %s", outputFile)
			logTool("ghidra", execErr.Error())
			report(2, Failed, time.Since(start), execErr, "ghidra")
			return execErr
		}

		logTool("ghidra", fmt.Sprintf("Ghidra decompiled to %s (%d bytes)", outputFile, info.Size()))

		// Parse function count from Ghidra script output
		funcCount := 0
		if result != nil {
			for _, line := range strings.Split(result.Stdout, "\n") {
				if n, err := fmt.Sscanf(line, "Morgue: Decompiled %d functions", &funcCount); n == 1 && err == nil {
					break
				}
			}
		}
		reportCount(2, time.Since(start), "ghidra", funcCount, "functions")
	}

	return nil
}
