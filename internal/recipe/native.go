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
		{Name: "Copy original", Required: true},
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

        println("Morgue: Decompiled " + count + " functions, " + errors + " errors -> " + outputPath);
    }
}
`

func (n *Native) Execute(ctx *Context) error {
	steps := n.Steps()
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
		r, _ := util.RunCmd(ctx.Ctx, stringsPath, []string{"-nobanner", "-accepteula", ctx.Target}, "")
		if r != nil {
			os.WriteFile(stringsOut, []byte(r.Stdout), 0644)
		}
		// Analyze and structure strings
		analyzeStrings(stringsOut, filepath.Join(ctx.Output, "strings.json"))
		report(1, Success, time.Since(start), nil)
	}

	// Step 2: Decompile with Ghidra
	report(2, Running, 0, nil)
	start = time.Now()
	ghidraPath, err := ctx.Tools.Resolve("ghidra")
	if err != nil {
		report(2, Failed, time.Since(start), fmt.Errorf("ghidra not available: %w", err))
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
		report(2, Failed, time.Since(start), err)
		return err
	}
	defer os.RemoveAll(projDir)

	// Write the export script to a temp file.
	// The filename MUST be MorgueExport.java — Ghidra requires the filename
	// to match the public class name inside the script.
	scriptDir, err := os.MkdirTemp("", "morgue-ghidra-script-*")
	if err != nil {
		report(2, Failed, time.Since(start), err)
		return err
	}
	defer os.RemoveAll(scriptDir)
	scriptPath := filepath.Join(scriptDir, "MorgueExport.java")
	scriptFile, err := os.Create(scriptPath)
	if err != nil {
		report(2, Failed, time.Since(start), err)
		return err
	}

	if _, err = scriptFile.WriteString(ghidraExportScript); err != nil {
		scriptFile.Close()
		report(2, Failed, time.Since(start), err)
		return err
	}
	scriptFile.Close()

	// Prepare output directory and file
	srcDir := filepath.Join(ctx.Output, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		report(2, Failed, time.Since(start), err)
		return err
	}

	baseName := strings.TrimSuffix(filepath.Base(ctx.Target), filepath.Ext(ctx.Target))
	outputFile := filepath.Join(srcDir, baseName+".c")

	// Run Ghidra analyzeHeadless
	log(fmt.Sprintf("Running Ghidra analyzeHeadless on %s", filepath.Base(ctx.Target)))
	result, err := util.RunCmd(ctx.Ctx, analyzeHeadless, []string{
		projDir, "MorgueProject",
		"-import", ctx.Target,
		"-postScript", scriptPath, outputFile,
		"-scriptPath", filepath.Dir(scriptPath),
		"-deleteProject",
	}, "")

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
		log(execErr.Error())
		report(2, Failed, time.Since(start), execErr)
		return execErr
	}

	// Verify output file exists and is non-empty
	info, err := os.Stat(outputFile)
	if err != nil || info.Size() == 0 {
		execErr := fmt.Errorf("ghidra produced no output at %s", outputFile)
		log(execErr.Error())
		report(2, Failed, time.Since(start), execErr)
		return execErr
	}

	log(fmt.Sprintf("Ghidra decompiled to %s (%d bytes)", outputFile, info.Size()))
	report(2, Success, time.Since(start), nil)

	return nil
}
