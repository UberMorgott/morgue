package recipe

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/provenance"
	"github.com/UberMorgott/morgue/internal/quality"
	"github.com/UberMorgott/morgue/internal/runmodel"
	"github.com/UberMorgott/morgue/internal/tools"
	"github.com/UberMorgott/morgue/internal/util"
	peparser "github.com/saferwall/pe"
)

// resolveGhidraJava returns the Java executable Ghidra should use, preferring
// the run-selected managed JDK under <BaseDir>/runtimes/java (system Java only
// with the prefer_system_tools opt-in).
// Returns "" if no Java can be resolved (caller proceeds with the inherited
// environment, matching prior behavior). m may be nil.
func resolveGhidraJava(m *tools.Manager) string {
	if m == nil {
		return ""
	}
	javaPath, err := m.RuntimePath(tools.RuntimeJava)
	if err != nil {
		return ""
	}
	return javaPath
}

// ghidraJavaMin reads application.java.min from <ghidraHome>/Ghidra/application.properties.
// Returns 0 when the file or key is missing or not an integer.
func ghidraJavaMin(ghidraHome string) int {
	v := readProperty(filepath.Join(ghidraHome, "Ghidra", "application.properties"), "application.java.min")
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return 0
	}
	return n
}

// javaVersionOutput runs `java -version` (the version banner goes to stderr).
// A var so tests can substitute it without a real JVM.
var javaVersionOutput = func(ctx context.Context, javaPath string) (string, error) {
	res, err := util.RunCmd(ctx, javaPath, []string{"-version"}, "")
	if res == nil {
		return "", err
	}
	return res.Stderr + "\n" + res.Stdout, err
}

// reJavaVersion matches the quoted version in `java -version` output, e.g.
// `openjdk version "25.0.1" 2025-10-21` or `java version "1.8.0_431"`.
var reJavaVersion = regexp.MustCompile(`version "([0-9]+)(?:\.([0-9]+))?`)

// parseJavaMajor returns the feature release of a `java -version` banner
// (1.8 -> 8, 25.0.1 -> 25), or 0 when it cannot be parsed.
func parseJavaMajor(out string) int {
	m := reJavaVersion.FindStringSubmatch(out)
	if m == nil {
		return 0
	}
	major, _ := strconv.Atoi(m[1])
	if major == 1 && m[2] != "" {
		major, _ = strconv.Atoi(m[2])
	}
	return major
}

// checkGhidraJava enforces the JDK Ghidra itself declares: application.java.min
// from its application.properties, and a JDK (javac next to java) because the
// headless launcher refuses a bare JRE. The selected runtime is frozen for the
// run, so a mismatch is reported up front instead of letting analyzeHeadless
// fail with an opaque launcher error.
func checkGhidraJava(ctx context.Context, ghidraHome, javaPath string) error {
	minMajor := ghidraJavaMin(ghidraHome)
	if minMajor == 0 {
		return nil // unknown requirement: let Ghidra's own launcher decide
	}
	if javaPath == "" {
		return fmt.Errorf("ghidra requires JDK %d+ (application.java.min=%d) but no Java runtime was selected for this run; "+
			"install the managed Java runtime (Tools page or `morgue tools install java`) or put JDK %d+ on PATH", minMajor, minMajor, minMajor)
	}
	out, err := javaVersionOutput(ctx, javaPath)
	major := parseJavaMajor(out)
	if major == 0 {
		return fmt.Errorf("ghidra requires JDK %d+ but the version of %s could not be determined: %v", minMajor, javaPath, err)
	}
	if major < minMajor {
		return fmt.Errorf("ghidra requires JDK %d+ (application.java.min=%d) but the selected Java %s is version %d", minMajor, minMajor, javaPath, major)
	}
	if _, jerr := os.Stat(filepath.Join(filepath.Dir(javaPath), javacName())); jerr != nil {
		return fmt.Errorf("ghidra requires a JDK %d+ but the selected Java %s (version %d) is a JRE without javac", minMajor, javaPath, major)
	}
	return nil
}

func javacName() string {
	if runtime.GOOS == "windows" {
		return "javac.exe"
	}
	return "javac"
}

// ghidraStatusFile is the per-function decompile status NDJSON MorgueExport.java
// writes next to the combined .c: one {"address","name","source","thunk",
// "status","warnings","undefinedReturn","message"} line per function Ghidra
// found, framed by a {"kind":"program"} header and a {"kind":"summary"} trailer.
// ghidraLogFile is the raw analyzeHeadless stdout/stderr.
const (
	ghidraStatusFile = "ghidra_functions.ndjson"
	ghidraLogFile    = "ghidra.log"
)

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
        // Optional second arg: per-function status NDJSON (one line per function
        // Ghidra found, whatever the decompile outcome) for measured quality.
        PrintWriter st = args.length > 1 ? new PrintWriter(new FileWriter(args[1])) : null;

        DecompInterface decomp = new DecompInterface();
        decomp.openProgram(currentProgram);

        FunctionIterator funcs = currentProgram.getListing().getFunctions(true);

        PrintWriter f = new PrintWriter(new FileWriter(outputPath));
        f.println("// Decompiled by Ghidra via Morgue");
        f.println("// PSEUDOCODE: Ghidra decompiler output reconstructed from machine code, not original source.");
        f.println("// Binary: " + currentProgram.getExecutablePath());
        f.println("// Architecture: " + currentProgram.getLanguage());
        f.println();

        if (st != null) {
            boolean pdb = false;
            try { pdb = currentProgram.getOptions("Program Information").getBoolean("PDB Loaded", false); } catch (Exception ignored) { }
            st.println("{\"kind\":\"program\",\"ghidraVersion\":" + js(ghidra.framework.Application.getApplicationVersion())
                + ",\"language\":" + js(String.valueOf(currentProgram.getLanguageID()))
                + ",\"imageBase\":" + js("0x" + Long.toHexString(currentProgram.getImageBase().getOffset()))
                + ",\"pdbLoaded\":" + pdb
                + ",\"functionCount\":" + currentProgram.getFunctionManager().getFunctionCount() + "}");
        }

        int found = 0;
        int count = 0;
        int errors = 0;
        while (funcs.hasNext()) {
            Function func = funcs.next();
            found++;
            String status;
            String message = "";
            int warnings = 0;
            boolean undefinedReturn = false;
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
                        String body = c.getC();
                        f.println(body);
                        f.println();
                        count++;
                        status = "decompiled";
                        for (int i = body.indexOf("WARNING:"); i >= 0; i = body.indexOf("WARNING:", i + 8)) { warnings++; }
                        String rt = func.getReturnType() == null ? "" : func.getReturnType().getName();
                        undefinedReturn = rt.startsWith("undefined");
                        System.out.println("Morgue:fn:" + count + ":" + func.getName());
                        System.out.flush();
                    } else {
                        f.println("// FAILED: " + func.getName() + " @ " + func.getEntryPoint() + " (no decompiled function)");
                        f.println();
                        errors++;
                        status = "empty";
                    }
                } else {
                    message = results == null ? "no result" : results.getErrorMessage();
                    f.println("// FAILED: " + func.getName() + " @ " + func.getEntryPoint());
                    f.println();
                    errors++;
                    status = "failed";
                }
            } catch (Exception e) {
                message = String.valueOf(e.getMessage());
                f.println("// ERROR: " + func.getName() + " @ " + func.getEntryPoint() + " — " + e.getMessage());
                f.println();
                errors++;
                status = "error";
            }
            if (st != null) {
                st.println("{\"address\":" + js("0x" + Long.toHexString(func.getEntryPoint().getOffset()))
                    + ",\"name\":" + js(func.getName())
                    + ",\"source\":" + js(String.valueOf(func.getSymbol().getSource()))
                    + ",\"thunk\":" + func.isThunk()
                    + ",\"status\":" + js(status)
                    + ",\"warnings\":" + warnings
                    + ",\"undefinedReturn\":" + undefinedReturn
                    + ",\"message\":" + js(message == null ? "" : message.trim()) + "}");
            }
            // Flush periodically so a JVM OOM mid-run still leaves a usable partial .c
            if (found % 200 == 0) { f.flush(); if (st != null) { st.flush(); } }
        }

        f.println();
        f.println("// Total: " + count + " functions decompiled, " + errors + " errors");
        f.close();
        if (st != null) {
            st.println("{\"kind\":\"summary\",\"found\":" + found + ",\"decompiled\":" + count + ",\"errors\":" + errors + "}");
            st.close();
        }
        decomp.dispose();

        System.out.println("Morgue: Decompiled " + count + " functions, " + errors + " errors -> " + outputPath);
        System.out.flush();
    }

    private static String js(String s) {
        if (s == null) { return "null"; }
        StringBuilder b = new StringBuilder("\"");
        for (int i = 0; i < s.length(); i++) {
            char ch = s.charAt(i);
            switch (ch) {
                case '"': b.append("\\\""); break;
                case '\\': b.append("\\\\"); break;
                case '\n': b.append("\\n"); break;
                case '\r': b.append("\\r"); break;
                case '\t': b.append("\\t"); break;
                default:
                    if (ch < 0x20) { b.append(String.format("\\u%04x", (int) ch)); } else { b.append(ch); }
            }
        }
        return b.append('"').toString();
    }
}
`

// runGhidra runs Ghidra's analyzeHeadless on binaryPath, decompiling every
// function to C via the embedded MorgueExport.java script. The decompiled C is
// written to <outDir>/<binaryBaseName>.c and the number of decompiled functions
// is returned.
//
// ghidraToolPath is the path resolved from the tools manager (ghidraRun.bat);
// the function locates support/analyzeHeadless relative to it.
//
// javaPath is the Java executable resolved from the runtime manager
// (managed JDK under <BaseDir>/runtimes/java preferred, else system java). It is
// used to point Ghidra's launch script at the correct JVM by setting JAVA_HOME
// and prepending its bin/ dir to PATH for the analyzeHeadless child process.
// Ghidra's .bat scripts only search the ambient PATH/JAVA_HOME for Java, so on a
// clean machine without system Java this is what lets Ghidra find the managed JDK.
// May be empty (e.g. resolution failed) — in that case the process inherits the
// unmodified environment, preserving prior behavior.
//
// onLog receives human-readable log lines (already tagged "ghidra"-style by the
// caller's logTool). onPhase reports progress as decompilation proceeds: name is
// either a phase marker ("ghidra:import" / "ghidra:analyze" / "ghidra:disassemble")
// or the current function name, and count is the running function count (0 until
// decompilation begins). Both callbacks may be nil.
func runGhidra(
	ctx context.Context,
	ghidraToolPath, javaPath, binaryPath, outDir string,
	limits ghidraLimits,
	onLog func(msg string),
	onPhase func(name string, count int),
	preScripts ...ghidraScript,
) (funcCount int, err error) {
	log := func(msg string) {
		if onLog != nil {
			onLog(msg)
		}
	}

	// Build the environment for analyzeHeadless. Ghidra's launch scripts find
	// Java via JAVA_HOME / the ambient PATH only, so point them at the resolved
	// JVM (managed JDK preferred, else system). javaPath is <home>/bin/java[.exe];
	// JAVA_HOME = its grandparent (the JDK/JRE home, i.e. parent of bin/).
	var ghidraEnv []string
	if javaPath != "" {
		javaBin := filepath.Dir(javaPath) // <home>/bin
		javaHome := filepath.Dir(javaBin) // <home>
		ghidraEnv = append(ghidraEnv,
			"JAVA_HOME="+javaHome,
			"PATH="+javaBin+string(os.PathListSeparator)+util.ChildPath(),
		)
		log(fmt.Sprintf("Using Java at %s (JAVA_HOME=%s)", javaPath, javaHome))
	}

	// Bound the JVM so one decompile cannot starve the machine: heap defaults to
	// min(8G, 25% RAM) (floor 2G, Ghidra's own default) and analysis threads to
	// half the CPUs; both configurable (ghidra_max_mem_gb / ghidra_max_cpu).
	// An explicit GHIDRA_HEADLESS_MAXMEM / GHIDRA_MAXMEM env still wins.
	totalGB := 0
	if totalBytes := util.TotalPhysicalMemoryBytes(); totalBytes > 0 {
		totalGB = int(totalBytes / (1024 * 1024 * 1024)) //nolint:gosec // G115: physical RAM in GB is far below MaxInt on any machine that can run Ghidra
	}
	heapGB, maxCPU := ghidraResources(limits, totalGB, runtime.NumCPU())
	if os.Getenv("GHIDRA_HEADLESS_MAXMEM") == "" && os.Getenv("GHIDRA_MAXMEM") == "" {
		ghidraEnv = append(ghidraEnv, fmt.Sprintf("GHIDRA_HEADLESS_MAXMEM=%dG", heapGB))
		log(fmt.Sprintf("Ghidra heap capped at %dG (of %dG physical RAM), %d analysis threads", heapGB, totalGB, maxCPU))
	}

	// ghidraToolPath points to ghidraRun.bat; we need support/analyzeHeadless
	ghidraDir := filepath.Dir(ghidraToolPath)
	if jerr := checkGhidraJava(ctx, ghidraDir, javaPath); jerr != nil {
		log(jerr.Error())
		return 0, jerr
	}
	analyzeHeadless := filepath.Join(ghidraDir, "support", "analyzeHeadless")
	if runtime.GOOS == "windows" {
		analyzeHeadless += ".bat"
	}

	// Create temp dir for Ghidra project files
	projDir, err := os.MkdirTemp("", "morgue-ghidra-*")
	if err != nil {
		return 0, err
	}
	defer func() { _ = os.RemoveAll(projDir) }()

	// Write the export script to a temp file.
	// The filename MUST be MorgueExport.java — Ghidra requires the filename
	// to match the public class name inside the script.
	scriptDir, err := os.MkdirTemp("", "morgue-ghidra-script-*")
	if err != nil {
		return 0, err
	}
	defer func() { _ = os.RemoveAll(scriptDir) }()
	scriptPath := filepath.Join(scriptDir, "MorgueExport.java")
	if err = os.WriteFile(scriptPath, []byte(ghidraExportScript), 0644); err != nil {
		return 0, err
	}
	// Pre-scripts run after import, before auto-analysis (e.g. to enable an
	// analyzer that is off by default). Same filename == class name rule.
	var preArgs []string
	for _, ps := range preScripts {
		if err = os.WriteFile(filepath.Join(scriptDir, ps.Name), []byte(ps.Source), 0644); err != nil {
			return 0, err
		}
		preArgs = append(preArgs, "-preScript", ps.Name)
	}

	// Prepare output directory and file
	if err = os.MkdirAll(outDir, 0755); err != nil {
		return 0, err
	}
	baseName := strings.TrimSuffix(filepath.Base(binaryPath), filepath.Ext(binaryPath))
	outputFile := filepath.Join(outDir, baseName+".c")
	statusFile := filepath.Join(outDir, ghidraStatusFile)

	// analyzeHeadless runs in a JVM, which is not long-path aware: an input or
	// output path near MAX_PATH fails with "<path> is not a valid directory or
	// file". Go's own \\?\ handling cannot help a child process, so stage the
	// binary (and the .c it produces) under a short temp dir, keeping the file
	// name — it appears in Ghidra's output — and move the result back after.
	runBinary, runOutput, runStatus := binaryPath, outputFile, statusFile
	if util.NearMaxPath(binaryPath) || util.NearMaxPath(outputFile) {
		stage, mkErr := os.MkdirTemp("", "morgue-gh-*")
		if mkErr != nil {
			return 0, mkErr
		}
		defer func() { _ = os.RemoveAll(stage) }()
		runBinary = filepath.Join(stage, filepath.Base(binaryPath))
		runOutput = filepath.Join(stage, baseName+".c")
		runStatus = filepath.Join(stage, ghidraStatusFile)
		if cpErr := copyFile(binaryPath, runBinary); cpErr != nil {
			return 0, fmt.Errorf("stage %s for ghidra: %w", filepath.Base(binaryPath), cpErr)
		}
		log(fmt.Sprintf("Output path is %d chars (near the %d-char Windows limit) — "+
			"running Ghidra in a short temp dir (%s)", len(outputFile), util.MaxPath, stage))
	}

	// Run Ghidra analyzeHeadless with streaming for real-time progress
	log(fmt.Sprintf("Running Ghidra analyzeHeadless on %s", filepath.Base(binaryPath)))
	ghidraFuncCount := 0
	ghidraPhaseIdx := 0 // 0=import, 1=analyze, 2=disassemble — only moves forward
	ghidraPhases := []string{"ghidra:import", "ghidra:analyze", "ghidra:disassemble"}
	lastLogTime := time.Now().Add(-2 * time.Second) // allow first log immediately
	// Launch with CREATE_BREAKAWAY_FROM_JOB so the JVM escapes morgue's Job
	// Object memory cap: GHIDRA_HEADLESS_MAXMEM above sizes the heap to ~70% of
	// physical RAM (tens of GB), far above the per-process cap, so without the
	// breakaway the JVM OOM-crashes on the large binaries we target.
	args := append([]string{projDir, "MorgueProject", "-import", runBinary}, preArgs...)
	args = append(args,
		"-max-cpu", strconv.Itoa(maxCPU),
		"-postScript", scriptPath, runOutput, runStatus,
		"-scriptPath", filepath.Dir(scriptPath),
		"-deleteProject",
	)
	result, runErr := util.RunCmdStreamingEnvBreakaway(ctx, ghidraEnv, analyzeHeadless, args, "", func(line string) {
		// Pre-script status lines ("Morgue:pre:<msg>") go straight to the log.
		if _, msg, ok := strings.Cut(line, "Morgue:pre:"); ok {
			log(msg)
			return
		}
		// Parse "Morgue:fn:<count>:<funcName>" from our export script
		if strings.HasPrefix(line, "Morgue:fn:") {
			parts := strings.SplitN(line, ":", 4)
			if len(parts) >= 4 {
				_, _ = fmt.Sscanf(parts[2], "%d", &ghidraFuncCount)
				if time.Since(lastLogTime) >= time.Second {
					log(fmt.Sprintf("Decompiled %d functions (%s)", ghidraFuncCount, parts[3]))
					if onPhase != nil {
						onPhase(parts[3], ghidraFuncCount)
					}
					lastLogTime = time.Now()
				}
			}
		} else if ghidraFuncCount == 0 {
			// Pre-decompilation: detect phase transitions (only forward, never back)
			lower := strings.ToLower(line)
			if ghidraPhaseIdx < 1 && (strings.Contains(lower, "analyz") || strings.Contains(lower, "analysis")) {
				ghidraPhaseIdx = 1
			} else if ghidraPhaseIdx < 2 && strings.Contains(lower, "disassembl") {
				ghidraPhaseIdx = 2
			}
			if time.Since(lastLogTime) >= time.Second {
				if onPhase != nil {
					onPhase(ghidraPhases[ghidraPhaseIdx], 0)
				}
				lastLogTime = time.Now()
			}
		}
	})

	exitCode := -1
	if result != nil {
		exitCode = result.ExitCode
		// Persist the raw tool log before anything parses it (quality and
		// provenance cite it as evidence). Logged-not-fatal.
		raw := "# analyzeHeadless stdout\n" + result.Stdout + "\n# analyzeHeadless stderr\n" + result.Stderr
		if werr := os.WriteFile(filepath.Join(outDir, ghidraLogFile), []byte(raw), 0644); werr != nil {
			log(fmt.Sprintf("write %s: %v", ghidraLogFile, werr))
		}
	}
	if runErr != nil || exitCode != 0 {
		stderr := ""
		if result != nil && result.Stderr != "" {
			stderr = result.Stderr
		}
		execErr := fmt.Errorf("ghidra analyzeHeadless failed (exit %d): %s", exitCode, stderr)
		log(execErr.Error())
		return 0, execErr
	}

	// Bring the staged .c back to its real (possibly long) home. Go handles the
	// long destination itself; a plain rename would fail across volumes.
	if runOutput != outputFile {
		if cpErr := copyFile(runOutput, outputFile); cpErr != nil {
			log(fmt.Sprintf("staged output copy back failed: %v", cpErr))
		}
		if fileExists(runStatus) {
			if cpErr := copyFile(runStatus, statusFile); cpErr != nil {
				log(fmt.Sprintf("staged status copy back failed: %v", cpErr))
			}
		}
	}

	// Verify output file exists and is non-empty
	info, statErr := os.Stat(util.LongPath(outputFile))
	if statErr != nil || info.Size() == 0 {
		// analyzeHeadless can exit 0 even when the postScript dies (e.g. a JVM
		// OutOfMemoryError on a huge binary): import/analyze already succeeded,
		// so the exit==0 check above passes. Surface the captured stderr so the
		// real cause is visible instead of a bare "no output".
		diag := ""
		if result != nil && result.Stderr != "" {
			s := result.Stderr
			if len(s) > 1200 {
				s = s[len(s)-1200:]
				if i := strings.IndexByte(s, '\n'); i >= 0 {
					s = s[i+1:]
				}
			}
			diag = "; ghidra stderr (tail): " + s
		}
		execErr := fmt.Errorf("ghidra produced no output at %s%s", outputFile, diag)
		log(execErr.Error())
		return 0, execErr
	}

	log(fmt.Sprintf("Ghidra decompiled to %s (%d bytes)", outputFile, info.Size()))

	// Parse function count (and failure count) from the script's final summary
	// line: "Morgue: Decompiled <count> functions, <errors> errors -> <path>".
	funcCount = 0
	errCount := 0
	if result != nil {
		for line := range strings.SplitSeq(result.Stdout, "\n") {
			// n counts successfully scanned verbs; the script always prints both
			// counts, but tolerate a truncated line by accepting n >= 1.
			if n, _ := fmt.Sscanf(line, "Morgue: Decompiled %d functions, %d errors", &funcCount, &errCount); n >= 1 {
				break
			}
		}
	}
	if errCount > 0 {
		log(fmt.Sprintf("Ghidra: %d functions decompiled, %d failed", funcCount, errCount))
	}
	return funcCount, nil
}

// indexEntry describes a single decompiled source file in index.json.
type indexEntry struct {
	Path  string `json:"path"`  // relative to the indexed output dir
	Size  int64  `json:"size"`  // bytes
	Lines int    `json:"lines"` // newline count (via countLines)
}

// outputIndex is the structure written to <outDir>/index.json by buildIndex.
type outputIndex struct {
	GeneratedAt string       `json:"generated_at"`
	FileCount   int          `json:"file_count"`
	TotalBytes  int64        `json:"total_bytes"`
	TotalLines  int          `json:"total_lines"`
	StringsLine int          `json:"strings_lines,omitempty"`
	Files       []indexEntry `json:"files"`
}

// sourceExts are the decompiled-source file extensions buildIndex catalogs.
var sourceExts = map[string]bool{
	".c": true, ".h": true, ".cpp": true, ".cs": true, ".java": true,
}

// assetExts are the extracted-asset file extensions the UE index catalogs in
// addition to source. These are binary game assets (not source), so their
// "lines" count is meaningless and recorded as 0.
var assetExts = map[string]bool{
	".uasset": true, ".uexp": true, ".umap": true, ".ubulk": true,
	".uptnl": true, ".ufont": true, ".bin": true, ".locres": true,
}

// buildIndex walks outDir, writes <outDir>/index.json listing every decompiled
// source file (path relative to outDir, size) plus aggregate counts. If a
// strings.txt exists at the root of outDir, its line count is recorded too.
// Returns the populated index for the caller to log/report.
func buildIndex(outDir string) (*outputIndex, error) {
	return buildIndexWith(outDir, nil, sourceExts)
}

// buildUEIndex builds an index that catalogs BOTH decompiled source under
// srcDir (if any) and extracted game assets under extractedDir (if any). The
// index.json is written to outDir (= ctx.Output) so it sits alongside both
// src/ and extracted/ rather than inside an empty src/. Source files keep their
// line counts; binary assets record 0 lines. Paths are relative to outDir.
func buildUEIndex(outDir, srcDir, extractedDir string) (*outputIndex, error) {
	exts := map[string]bool{}
	for e := range sourceExts {
		exts[e] = true
	}
	for e := range assetExts {
		exts[e] = true
	}
	var roots []string
	if srcDir != "" {
		roots = append(roots, srcDir)
	}
	if extractedDir != "" {
		roots = append(roots, extractedDir)
	}

	// F4: emit Go-native .uasset semantics (assets_index.json + assets.ndjson)
	// from the extracted package tree. Logged-not-fatal (like analyzeStrings):
	// a parser hiccup must never fail the index build. Side-effect only — the
	// outputIndex shape (asserted by ghidra_test.go) is untouched.
	if extractedDir != "" {
		if ai, err := buildAssetsIndex(outDir, extractedDir); err != nil {
			log.Printf("uasset index: %v", err)
		} else if ai != nil {
			log.Printf("uasset index: parsed %d assets (%d failed), %d names -> assets_index.json",
				ai.AssetsParsed, ai.AssetsFailed, ai.TotalNames)
		}
	}

	return buildIndexWith(outDir, roots, exts)
}

// buildIndexWith is the shared implementation. It walks each root in roots
// (defaulting to outDir itself when roots is empty), catalogs files whose
// extension is in exts, writes <outDir>/index.json, and returns the index.
// Line counts are only computed for source extensions; binary assets get 0.
func buildIndexWith(outDir string, roots []string, exts map[string]bool) (*outputIndex, error) {
	idx := &outputIndex{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Files:       []indexEntry{},
	}
	if len(roots) == 0 {
		roots = []string{outDir}
	}

	seen := map[string]bool{}
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil //nolint:nilerr // an unreadable entry is left out of the index; the walk must still cover the rest
			}
			ext := strings.ToLower(filepath.Ext(path))
			if !exts[ext] {
				return nil
			}
			rel, relErr := filepath.Rel(outDir, path)
			if relErr != nil {
				rel = path
			}
			rel = filepath.ToSlash(rel)
			if seen[rel] {
				return nil
			}
			seen[rel] = true
			info, infoErr := d.Info()
			if infoErr != nil {
				return nil //nolint:nilerr // a file that vanished mid-walk is left out of the index; the walk must still cover the rest
			}
			lines := 0
			if sourceExts[ext] {
				lines = countLines(path)
			}
			idx.Files = append(idx.Files, indexEntry{
				Path:  rel,
				Size:  info.Size(),
				Lines: lines,
			})
			idx.FileCount++
			idx.TotalBytes += info.Size()
			idx.TotalLines += lines
			return nil
		})
	}

	// strings.txt may sit at the indexed dir root (legacy) or in its parent
	// (ctx.Output), since recipes write it next to src/. Prefer the local one,
	// fall back to the parent; record line count only if one exists.
	if stringsTxt := filepath.Join(outDir, "strings.txt"); fileExists(stringsTxt) {
		idx.StringsLine = countLines(stringsTxt)
	} else if parentStrings := filepath.Join(filepath.Dir(outDir), "strings.txt"); fileExists(parentStrings) {
		idx.StringsLine = countLines(parentStrings)
	}

	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(outDir, "index.json"), data, 0644); err != nil {
		return nil, err
	}
	return idx, nil
}

// fileExists reports whether path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// ghidraProvenance streams provenance for one Ghidra-decompiled module under
// srcDir (= <out>/src): artifact rows for the combined pseudocode, the status
// and split catalogs and the raw log, one row per function Ghidra found
// (address + RVA identity, Ghidra's own name and its SourceType), and one row
// per single-string-candidate rename from indexes/name_map.csv. Streaming: the
// status file and functions.ndjson are merged in their shared Ghidra order.
// A nil sink is a no-op. Returns the number of records written.
func ghidraProvenance(sink provenance.Sink, out, srcDir, binary, fallbackVersion string, nativeAOT bool) (int, error) {
	if sink == nil {
		return 0, nil
	}
	module := filepath.Base(binary)
	rel := func(p string) string {
		r, err := filepath.Rel(out, p)
		if err != nil {
			return filepath.ToSlash(p)
		}
		return filepath.ToSlash(r)
	}
	statusPath := filepath.Join(srcDir, ghidraStatusFile)
	statusRel := rel(statusPath)
	baseName := strings.TrimSuffix(module, filepath.Ext(module))
	combined := filepath.Join(srcDir, baseName+".c")
	combinedRel := rel(combined)

	// Program header first: observed Ghidra version and image base.
	version, imageBase, pdbLoaded := fallbackVersion, uint64(0), false
	sf, err := os.Open(statusPath) //nolint:gosec // G304: fixed file under the pipeline's own output tree
	if err != nil {
		if os.IsNotExist(err) {
			sf = nil
		} else {
			return 0, err
		}
	}
	var sr *bufio.Reader
	if sf != nil {
		defer func() { _ = sf.Close() }()
		sr = bufio.NewReaderSize(sf, scannerInitBuf)
	}
	nextStatus := func() (quality.GhidraFunction, bool) {
		for sr != nil {
			b, rerr := sr.ReadBytes('\n')
			if len(b) > 0 {
				var g quality.GhidraFunction
				if json.Unmarshal(b, &g) == nil {
					return g, true
				}
			}
			if rerr != nil {
				return quality.GhidraFunction{}, false
			}
		}
		return quality.GhidraFunction{}, false
	}
	first, ok := nextStatus()
	if ok && first.Kind == "program" {
		if first.GhidraVersion != "" {
			version = first.GhidraVersion
		}
		if v, perr := strconv.ParseUint(strings.TrimPrefix(first.ImageBase, "0x"), 16, 64); perr == nil {
			imageBase = v
		}
		pdbLoaded = first.PDBLoaded
		first, ok = nextStatus()
	}
	exports := peExportRVAs(binary)
	producer := runmodel.ProvenanceProducer{Tool: "ghidra", Version: version, StepID: "native.decompile",
		Args: []string{"analyzeHeadless", "-import", module, "-postScript", "MorgueExport.java"}}
	strp := func(s string) *string { return &s }
	written := 0
	write := func(r runmodel.ProvenanceRecord) error {
		r.SchemaVersion = runmodel.ProvenanceVersion
		r.Module = module
		if r.Evidence == nil {
			r.Evidence = []runmodel.Evidence{}
		}
		if err := sink.Write(r); err != nil {
			return err
		}
		written++
		return nil
	}

	artifacts := []struct{ path, kind, detail string }{
		{combined, runmodel.FidelityPseudocode, "combined Ghidra decompiler output (pseudocode, not original source)"},
		{statusPath, "index", "per-function decompile status from MorgueExport.java"},
		{filepath.Join(srcDir, "functions.ndjson"), "index", "split function catalog"},
		{filepath.Join(srcDir, ghidraLogFile), "log", "raw analyzeHeadless stdout/stderr"},
	}
	for _, a := range artifacts {
		if !fileExists(a.path) {
			continue
		}
		p := rel(a.path)
		if err := write(runmodel.ProvenanceRecord{ID: module + ":artifact:" + p, ArtifactPath: p, ArtifactKind: a.kind,
			NameSource: "unknown", Confidence: "unknown", Producer: producer,
			Evidence: []runmodel.Evidence{{Path: p, Detail: a.detail}}}); err != nil {
			return written, err
		}
	}

	// Split catalog reader: one entry per decompiled function, same order.
	var fr *bufio.Reader
	if ff, ferr := os.Open(filepath.Join(srcDir, "functions.ndjson")); ferr == nil { //nolint:gosec // G304: fixed file under the pipeline's own output tree
		defer func() { _ = ff.Close() }()
		fr = bufio.NewReaderSize(ff, scannerInitBuf)
	}
	nextSplit := func() (funcEntry, bool) {
		for fr != nil {
			b, rerr := fr.ReadBytes('\n')
			if len(b) > 0 {
				var e funcEntry
				if json.Unmarshal(b, &e) == nil {
					return e, true
				}
			}
			if rerr != nil {
				return funcEntry{}, false
			}
		}
		return funcEntry{}, false
	}

	for g := first; ok; g, ok = nextStatus() {
		if g.Kind != "" {
			continue
		}
		addr := quality.NormalizeAddr(g.Address)
		r := runmodel.ProvenanceRecord{ID: module + ":fn:" + addr, ArtifactPath: combinedRel,
			ArtifactKind: runmodel.FidelityPseudocode, Producer: producer}
		if addr != "" {
			r.Address = strp(addr)
			if v, perr := strconv.ParseUint(strings.TrimPrefix(addr, "0x"), 16, 64); perr == nil && imageBase > 0 && v >= imageBase {
				r.RVA = strp("0x" + strconv.FormatUint(v-imageBase, 16))
			}
		}
		if g.Status == "decompiled" {
			if e, sok := nextSplit(); sok && quality.NormalizeAddr(e.Address) == addr {
				r.ArtifactPath = "src/" + e.File
				if p := rel(filepath.Join(srcDir, filepath.FromSlash(e.File))); p != "" {
					r.ArtifactPath = p
				}
			}
		}
		detail := fmt.Sprintf("Ghidra %s; SourceType %s; thunk=%t", g.Status, g.Source, g.Thunk)
		if g.Message != "" {
			detail += "; " + g.Message
		}
		r.Evidence = []runmodel.Evidence{{Path: statusRel, Detail: detail}}
		name := g.Name
		switch g.Source {
		case "IMPORTED":
			r.OriginalName = strp(name)
			switch {
			case g.Thunk:
				r.NameSource, r.Confidence = "metadata", "high" // import table thunk
			case r.RVA != nil && exports[*r.RVA]:
				r.NameSource, r.Confidence = "export", "high"
			case pdbLoaded:
				r.NameSource, r.Confidence = "debug-symbol", "high"
			default:
				// COFF/Go symbol table, entry label or similar: carried in the
				// binary, but not proven to be an exported name.
				r.NameSource, r.Confidence = "debug-symbol", "medium"
				r.Evidence[0].Detail += "; not in the PE export table, no PDB: binary symbol table or loader label"
			}
		case "USER_DEFINED":
			r.RecoveredName = strp(name)
			if nativeAOT {
				r.NameSource, r.Confidence = "metadata", "medium" // ghidra-nativeaot runtime metadata
			} else {
				r.NameSource, r.Confidence = "heuristic", "low"
			}
		case "ANALYSIS":
			r.RecoveredName = strp(name)
			r.NameSource, r.Confidence = "heuristic", "medium" // Ghidra analyzers (FID, RTTI, demangler)
		default:
			// DEFAULT: FUN_<addr> is a generated placeholder, not a recovered name.
			r.NameSource, r.Confidence = "unknown", "unknown"
		}
		if err := write(r); err != nil {
			return written, err
		}
	}

	// Renames by the conservative single-string-candidate rule (nameres.go).
	nmPath := filepath.Join(srcDir, "indexes", "name_map.csv")
	if nf, nerr := os.Open(nmPath); nerr == nil { //nolint:gosec // G304: fixed file under the pipeline's own output tree
		defer func() { _ = nf.Close() }()
		cr := csv.NewReader(bufio.NewReader(nf))
		cr.FieldsPerRecord = -1
		renameProducer := runmodel.ProvenanceProducer{Tool: "morgue", Version: "nameres-v1", StepID: "native.rename", Args: []string{}}
		nmRel, refsRel := rel(nmPath), rel(filepath.Join(srcDir, "indexes", "string_refs.csv"))
		header := true
		for {
			rec, rerr := cr.Read()
			if rerr == io.EOF {
				break
			}
			if rerr != nil {
				return written, rerr
			}
			if header || len(rec) < 3 {
				header = false
				continue
			}
			addr := quality.NormalizeAddr(rec[0])
			r := runmodel.ProvenanceRecord{ID: module + ":rename:" + addr, ArtifactPath: nmRel, ArtifactKind: "index",
				RecoveredName: strp(rec[2]), NameSource: "single-string-candidate", Confidence: "medium",
				Producer: renameProducer,
				Evidence: []runmodel.Evidence{
					{Path: refsRel, Detail: fmt.Sprintf("function references exactly one qualified C++ identifier string %q", rec[2])},
					{Path: nmRel, Detail: "Ghidra default name " + rec[1] + " replaced; it was a placeholder, not an original name"},
				}}
			if addr != "" {
				r.Address = strp(addr)
			}
			if err := write(r); err != nil {
				return written, err
			}
		}
	}
	return written, nil
}

// ghidraVersion is the run-selected Ghidra version (fallback when the status
// file's observed version is absent). "" when unknown, never "latest".
func ghidraVersion(ctx *Context) string {
	if ctx == nil || ctx.Tools == nil {
		return ""
	}
	if v := ctx.Tools.Check("ghidra").Version; v != "latest" {
		return v
	}
	return ""
}

// peExportRVAs returns the PE export table function RVAs of binary as
// normalized 0x-hex strings (bounded by the export count). Empty for a
// non-PE, unreadable or export-less file.
func peExportRVAs(binary string) map[string]bool {
	set := map[string]bool{}
	f, err := peparser.New(util.LongPath(binary), nil)
	if err != nil {
		return set
	}
	defer func() { _ = f.Close() }()
	if f.Parse() != nil {
		return set
	}
	for _, fn := range f.Export.Functions {
		if fn.FunctionRVA != 0 && fn.Forwarder == "" {
			set["0x"+strconv.FormatUint(uint64(fn.FunctionRVA), 16)] = true
		}
	}
	return set
}

// ghidraLimits are the configured analyzeHeadless resource caps (0 = auto).
type ghidraLimits struct {
	MaxMemGB int
	MaxCPU   int
}

// ghidraLimitsFrom reads the caps from the run config (nil = auto).
func ghidraLimitsFrom(cfg *config.Config) ghidraLimits {
	if cfg == nil {
		return ghidraLimits{}
	}
	return ghidraLimits{MaxMemGB: cfg.GhidraMaxMemGB, MaxCPU: cfg.GhidraMaxCPU}
}

// ghidraResources resolves the JVM heap (GB) and -max-cpu for analyzeHeadless.
// Auto heap = min(8, 25% of totalGB), floor 2 (Ghidra's default); an explicit
// cap is honoured but floored at 1 and never above totalGB-2 when RAM is known.
// Auto CPU = half the logical CPUs, at least 1; explicit is clamped to [1,cpus].
func ghidraResources(l ghidraLimits, totalGB, cpus int) (heapGB, maxCPU int) {
	if l.MaxMemGB > 0 {
		heapGB = l.MaxMemGB
		if totalGB > 3 {
			heapGB = min(heapGB, totalGB-2)
		}
		heapGB = max(heapGB, 1)
	} else {
		heapGB = max(min(8, totalGB/4), 2)
	}
	cpus = max(cpus, 1)
	if l.MaxCPU > 0 {
		maxCPU = min(l.MaxCPU, cpus)
	} else {
		maxCPU = max(cpus/2, 1)
	}
	return heapGB, maxCPU
}
