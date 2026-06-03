package recipe

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/UberMorgott/morgue/internal/tools"
	"github.com/UberMorgott/morgue/internal/util"
)

// resolveGhidraJava returns the Java executable Ghidra should use, preferring
// the managed JDK under <BaseDir>/runtimes/java and falling back to system java.
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
            // Flush periodically so a JVM OOM mid-run still leaves a usable partial .c
            if ((count + errors) % 200 == 0) { f.flush(); }
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
	onLog func(msg string),
	onPhase func(name string, count int),
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
			"PATH="+javaBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		)
		log(fmt.Sprintf("Using Java at %s (JAVA_HOME=%s)", javaPath, javaHome))
	}

	// Ghidra's headless launcher forces a 2G heap, which OOMs on large binaries
	// (a ~278MB binary needs tens of GB just for auto-analysis). When the user
	// has not set a heap override, scale one to ~70% of physical RAM, leaving
	// headroom for the OS and flooring at the 2G default.
	if os.Getenv("GHIDRA_HEADLESS_MAXMEM") == "" && os.Getenv("GHIDRA_MAXMEM") == "" {
		if totalBytes := util.TotalPhysicalMemoryBytes(); totalBytes > 0 {
			totalGB := int(totalBytes / (1024 * 1024 * 1024))
			heapGB := min(totalGB*70/100, totalGB-3)
			if heapGB < 2 {
				heapGB = 2
			}
			ghidraEnv = append(ghidraEnv, fmt.Sprintf("GHIDRA_HEADLESS_MAXMEM=%dG", heapGB))
			log(fmt.Sprintf("Ghidra heap set to %dG (of %dG physical RAM)", heapGB, totalGB))
		}
	}

	// ghidraToolPath points to ghidraRun.bat; we need support/analyzeHeadless
	ghidraDir := filepath.Dir(ghidraToolPath)
	analyzeHeadless := filepath.Join(ghidraDir, "support", "analyzeHeadless")
	if runtime.GOOS == "windows" {
		analyzeHeadless += ".bat"
	}

	// Create temp dir for Ghidra project files
	projDir, err := os.MkdirTemp("", "morgue-ghidra-*")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(projDir)

	// Write the export script to a temp file.
	// The filename MUST be MorgueExport.java — Ghidra requires the filename
	// to match the public class name inside the script.
	scriptDir, err := os.MkdirTemp("", "morgue-ghidra-script-*")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(scriptDir)
	scriptPath := filepath.Join(scriptDir, "MorgueExport.java")
	if err = os.WriteFile(scriptPath, []byte(ghidraExportScript), 0644); err != nil {
		return 0, err
	}

	// Prepare output directory and file
	if err = os.MkdirAll(outDir, 0755); err != nil {
		return 0, err
	}
	baseName := strings.TrimSuffix(filepath.Base(binaryPath), filepath.Ext(binaryPath))
	outputFile := filepath.Join(outDir, baseName+".c")

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
	result, runErr := util.RunCmdStreamingEnvBreakaway(ctx, ghidraEnv, analyzeHeadless, []string{
		projDir, "MorgueProject",
		"-import", binaryPath,
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

	// Verify output file exists and is non-empty
	info, statErr := os.Stat(outputFile)
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
	".c": true, ".h": true, ".cpp": true, ".cs": true,
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
		filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
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
				return nil
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
