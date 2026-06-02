package recipe

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/UberMorgott/morgue/internal/recon"
	"github.com/UberMorgott/morgue/internal/util"
)

// reCSharpStringLit matches a simple double-quoted C# string literal (no
// interpolation/verbatim handling — good enough to harvest decrypted plaintext
// from decompiled source for the per-child strings.json). Escaped quotes inside
// are tolerated via the \\. alternation.
var reCSharpStringLit = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)

// DotnetConfuserEx handles ConfuserEx-obfuscated .NET assemblies.
type DotnetConfuserEx struct{}

func init() {
	RegisterFirst(&DotnetConfuserEx{})
}

func (d *DotnetConfuserEx) Name() string        { return "dotnet-confuserex" }
func (d *DotnetConfuserEx) Description() string { return "Deobfuscate an obfuscated .NET assembly (de4dot)" }

// Match handles any obfuscated managed assembly de4dot can attempt: the
// ConfuserEx family by name, and the generic "Obfuscated" value from the
// family-agnostic recon layer (de4dot auto-detect). Clean assemblies
// (Obfuscator=="") fall through to dotnet-generic.
func (d *DotnetConfuserEx) Match(r *recon.Result) bool {
	if r.Kind != recon.Managed {
		return false
	}
	lower := strings.ToLower(r.Obfuscator)
	return strings.Contains(lower, "confuser") || strings.Contains(lower, "obfuscated")
}

// isConfuserEx reports whether the recon obfuscator name is specifically the
// ConfuserEx family (vs the generic "Obfuscated" value). Drives de4dot flag
// selection: forced `-p crx` for ConfuserEx, auto-detect otherwise.
func isConfuserEx(obf string) bool {
	return strings.Contains(strings.ToLower(obf), "confuser")
}

func (d *DotnetConfuserEx) Steps() []StepInfo {
	// de4dot-cex performs the full ConfuserEx deobfuscation (anti-tamper, string
	// decryption, control-flow, proxy-call removal, renaming), so the standalone
	// NoFuserEx / AntiTamperKiller / proxy-remover passes are redundant and were
	// dropped (they are also broken headless: NoFuserEx needs a real console and
	// the AntiTamperKiller package ships without its runtimeconfig).
	return []StepInfo{
		{Name: "Copy original", Required: false},
		{Name: "Deobfuscate", Required: false},
		{Name: "Verify deobfuscation", Required: false},
		{Name: "Extract strings", Required: false},
		{Name: "Extract embedded", Required: false},
		{Name: "Recurse extracted", Required: false},
		{Name: "Decompile", Required: true},
		{Name: "Build indexes", Required: false},
	}
}

func (d *DotnetConfuserEx) RequiredTools() []string {
	return []string{"ilspycmd", "strings", "de4dot-cex"}
}

// DisplayTools lists the tools this recipe runs, in execution order, for the UI.
// Includes cfxextract and cfxstrings (built on demand, not downloadable
// RequiredTools) so the panel shows all participating tools from the start rather
// than adding them mid-run. de4dot-cex and cfxstrings also run again per extracted
// child during the recurse step, but each tool is shown once.
func (d *DotnetConfuserEx) DisplayTools() []string {
	return []string{"de4dot-cex", "strings", "cfxextract", "cfxstrings", "ilspycmd"}
}

func (d *DotnetConfuserEx) Execute(ctx *Context) error {
	steps := d.Steps()
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

	interDir := filepath.Join(ctx.Output, "intermediate")
	if err := os.MkdirAll(interDir, 0755); err != nil {
		return fmt.Errorf("create intermediate dir: %w", err)
	}

	// current tracks the working copy through the pipeline
	current := ctx.Target

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
	origCopy := filepath.Join(origDir, filepath.Base(ctx.Target))
	if err := copyFile(ctx.Target, origCopy); err != nil {
		report(0, Failed, time.Since(start), err, "")
		return err
	}
	report(0, Success, time.Since(start), nil, "")

	// Step 1: Deobfuscate (de4dot-cex).
	//
	// Flag selection depends on the recon-detected family:
	//   - ConfuserEx → forced `-p crx`. de4dot's auto-detect returns "Unknown" on
	//     marker-stripped ConfuserEx builds (DataLoader.dll) and then skips the
	//     ConfuserEx string decrypter, leaving \ueXXXX strings. Forcing crx runs
	//     the static string decrypter so strings come out readable.
	//   - generic "Obfuscated" → no `-p`, de4dot auto-detects the obfuscator
	//     itself; if it can't, it best-effort no-ops and we fall back to a plain
	//     decompile.
	// `--dont-rename` (both paths) keeps the real type/method names and namespace
	// layout, producing the SAME output structure as the generic recipe (de4dot's
	// default renaming flattens everything to Class0..ClassN and destroys names
	// like CreatePersistentToken). Control-flow deobfuscation runs by default.
	deobfArgs := []string{"--dont-rename"}
	if isConfuserEx(ctx.Obfuscator) {
		deobfArgs = []string{"-p", "crx", "--dont-rename"}
	}
	deobf := func(toolPath, input, output string) error {
		args := append([]string{input}, append(append([]string{}, deobfArgs...), "-o", output)...)
		r, err := util.RunCmd(ctx.Ctx, toolPath, args, "")
		if err != nil {
			return err
		}
		if r.ExitCode != 0 {
			return fmt.Errorf("de4dot-cex exit %d: %s", r.ExitCode, r.Stderr)
		}
		return nil
	}
	current = d.runToolStep(ctx, 1, current, interDir, "de4dot-cex", deobf, report, logTool)

	// Step 2: Verify deobfuscation. Instead of blindly re-running de4dot, scan the
	// step-1 stage's #US (user-string) heap for residual encrypted-string markers
	// (Private-Use-Area chars, U+E000..U+F8FF — the \ueXXXX literals ConfuserEx
	// string encryption leaves when decryption didn't happen). Clean → Success;
	// residual found → surface a warning (Skipped) so the run is not silently
	// reported as fully deobfuscated. (The decompiled source is scanned again
	// after step 5 — some configs only reveal residual strings post-decompile.)
	report(2, Running, 0, nil, "de4dot-cex")
	start = time.Now()
	if residual, _, verr := recon.CountPUAUserStrings(current); verr != nil {
		logTool("de4dot-cex", fmt.Sprintf("verification skipped: %v", verr))
		report(2, Skipped, time.Since(start), nil, "de4dot-cex")
	} else if residual > 0 {
		logTool("de4dot-cex", fmt.Sprintf("WARNING: %d residual encrypted (\\ueXXXX) strings remain — string decryption may be incomplete", residual))
		report(2, Skipped, time.Since(start), nil, "de4dot-cex")
	} else {
		logTool("de4dot-cex", "verified: no residual encrypted strings in deobfuscated assembly")
		report(2, Success, time.Since(start), nil, "de4dot-cex")
	}

	// Step 3: Extract strings
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
		r, _ := util.RunCmdStreaming(ctx.Ctx, stringsPath, []string{"-nobanner", "-accepteula", current}, "", func(line string) {
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
		if r != nil {
			os.WriteFile(stringsOut, []byte(r.Stdout), 0644)
		}
		// Analyze and structure strings
		analyzeStrings(stringsOut, filepath.Join(ctx.Output, "strings.json"))
		strCount := countLines(stringsOut)
		reportCount(3, time.Since(start), "strings", strCount, "strings")
	}

	// Step 4: Extract embedded ConfuserEx resources.
	//
	// The embedded assemblies live in a custom-encrypted ConfuserEx resource and
	// can only be decrypted by the target's own <Module> cctor at runtime. We load
	// the ORIGINAL target in-process (ctx.Target, NOT the de4dot-processed stage,
	// which can break anti-tamper/cctor), force its module cctor, and capture the
	// decrypted bytes via a Harmony prefix on Assembly.Load. This EXECUTES TARGET
	// CODE, so it is gated behind --allow-dynamic.
	report(4, Running, 0, nil, "cfxextract")
	start = time.Now()
	var extractedDLLs []string
	if !ctx.AllowDynamic {
		logTool("cfxextract", "embedded extraction requires --allow-dynamic (executes target code); skipping")
		report(4, Skipped, time.Since(start), nil, "cfxextract")
	} else {
		count, dlls, err := d.extractEmbedded(ctx, logTool)
		if err != nil {
			logTool("cfxextract", fmt.Sprintf("embedded extraction skipped: %v", err))
			report(4, Skipped, time.Since(start), nil, "cfxextract")
		} else {
			extractedDLLs = dlls
			reportCount(4, time.Since(start), "cfxextract", count, "assemblies")
		}
	}

	// Step 5: Recurse into extracted embedded assemblies. For each extracted
	// child run the full deobf+decrypt+decompile sub-pipeline (de4dot -p crx →
	// cfxstrings custom-string-decryptor → ilspycmd -p) into a per-child dir.
	// Only reachable when --allow-dynamic produced extracted assemblies.
	report(5, Running, 0, nil, "cfxstrings")
	start = time.Now()
	if len(extractedDLLs) == 0 {
		report(5, Skipped, time.Since(start), nil, "cfxstrings")
	} else {
		done := d.decompileExtracted(ctx, extractedDLLs, report, logTool)
		reportCount(5, time.Since(start), "cfxstrings", done, "assemblies")
	}

	// Step 6: Decompile (host assembly)
	report(6, Running, 0, nil, "ilspycmd")
	start = time.Now()
	ilspyPath, err := ctx.Tools.Resolve("ilspycmd")
	if err != nil {
		report(6, Failed, time.Since(start), err, "ilspycmd")
		return fmt.Errorf("ilspycmd not available: %w", err)
	}
	srcDir := filepath.Join(ctx.Output, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		report(6, Failed, time.Since(start), err, "ilspycmd")
		return fmt.Errorf("create src dir: %w", err)
	}
	ilspyArgs := []string{"-p", "-o", srcDir, current}
	if ctx.Config.CSharpLanguageVersion != "Auto" && ctx.Config.CSharpLanguageVersion != "" {
		ilspyArgs = append(ilspyArgs, "--languageversion", ctx.Config.CSharpLanguageVersion)
	}
	ilspyBin, ilspyRun := dotnetExec(ctx.Ctx, ilspyPath, ilspyArgs)
	result, runErr := util.RunCmd(ctx.Ctx, ilspyBin, ilspyRun, "")
	exitCode := -1
	if result != nil {
		exitCode = result.ExitCode
	}
	if runErr != nil || exitCode != 0 {
		stderr := ""
		if result != nil && result.Stderr != "" {
			stderr = result.Stderr
		}
		execErr := fmt.Errorf("ilspycmd failed (exit %d): %s", exitCode, stderr)
		report(6, Failed, time.Since(start), execErr, "ilspycmd")
		return execErr
	}
	csCount := countFilesWithExt(srcDir, ".cs")
	reportCount(6, time.Since(start), "ilspycmd", csCount, "types")

	// Post-decompile residual-string check: ConfuserEx string encryption that
	// de4dot did not undo surfaces as \ueXXXX (Private-Use-Area) literals in the
	// decompiled C#. Warn if any remain so the result is not silently trusted.
	if pua := countPUAInSource(srcDir); pua > 0 {
		logTool("ilspycmd", fmt.Sprintf("WARNING: %d residual encrypted (\\ueXXXX) chars in decompiled source — string decryption incomplete", pua))
	} else if csCount > 0 {
		logTool("ilspycmd", "verified: decompiled source contains no residual encrypted strings")
	}

	// Step 7: Build indexes
	report(7, Running, 0, nil, "")
	start = time.Now()
	logTool("ilspycmd", "Building indexes for decompiled output")
	if _, statErr := os.Stat(srcDir); statErr != nil {
		logTool("ilspycmd", "No source to index — ilspycmd produced no src/ output, skipping")
		report(7, Skipped, time.Since(start), nil, "")
	} else if idx, err := buildIndex(srcDir); err != nil {
		logTool("ilspycmd", fmt.Sprintf("Build indexes failed: %v", err))
		report(7, Failed, time.Since(start), err, "")
	} else {
		logTool("ilspycmd", fmt.Sprintf("Indexed %d source files (%d bytes) -> index.json", idx.FileCount, idx.TotalBytes))
		reportCount(7, time.Since(start), "", idx.FileCount, "files")
	}

	return nil
}

// countPUAInSource counts Private-Use-Area characters (U+E000..U+F8FF) across
// all .cs files under dir. These are the \ueXXXX literals left by ConfuserEx
// string encryption that de4dot did not decrypt. A non-zero result means string
// decryption was incomplete.
func countPUAInSource(dir string) int {
	total := 0
	filepath.WalkDir(dir, func(path string, de os.DirEntry, err error) error {
		if err != nil || de.IsDir() || filepath.Ext(path) != ".cs" {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		for _, r := range string(data) {
			if r >= 0xE000 && r <= 0xF8FF {
				total++
			}
		}
		return nil
	})
	return total
}

// runToolStep runs a single tool step with fallback (copies as-is on failure).
func (d *DotnetConfuserEx) runToolStep(
	ctx *Context, stepIdx int, current, interDir, toolName string,
	run func(toolPath, input, output string) error,
	report func(int, StepStatus, time.Duration, error, string),
	logTool func(string, string),
) string {
	report(stepIdx, Running, 0, nil, toolName)
	start := time.Now()

	toolPath, err := ctx.Tools.Resolve(toolName)
	if err != nil {
		logTool(toolName, fmt.Sprintf("%s not available: %v", toolName, err))
		report(stepIdx, Skipped, time.Since(start), nil, toolName)
		return current
	}
	// Resolve can hand back the expected install path even when the tool was
	// never downloaded. Treat a missing binary as Skipped (not installed), not
	// Failed — these are best-effort passes that fall back to the previous stage.
	if _, statErr := os.Stat(toolPath); statErr != nil {
		logTool(toolName, fmt.Sprintf("%s not installed — skipping", toolName))
		report(stepIdx, Skipped, time.Since(start), nil, toolName)
		return current
	}

	output := filepath.Join(interDir, fmt.Sprintf("step%d_%s%s", stepIdx, toolName, filepath.Ext(current)))
	if err := run(toolPath, current, output); err != nil {
		// These are best-effort passes (anti-tamper, proxy removal, ...) that may
		// not support every binary. A failure is non-fatal — fall back to the
		// previous stage and report Skipped rather than a hard (red) Failed.
		logTool(toolName, fmt.Sprintf("%s did not apply: %v — using previous stage", toolName, err))
		report(stepIdx, Skipped, time.Since(start), nil, toolName)
		return current // fallback: keep using previous stage
	}

	// Verify output exists
	fi, err := os.Stat(output)
	if err != nil {
		logTool(toolName, fmt.Sprintf("%s produced no output — using previous stage", toolName))
		report(stepIdx, Skipped, time.Since(start), nil, toolName)
		return current
	}

	// Report success with the output size (KB) as a stat. Deobfuscation passes
	// have no natural item count, so this gives them a number next to their
	// indicator instead of a bare checkmark.
	if ctx.Progress != nil {
		steps := d.Steps()
		name := toolName
		if stepIdx < len(steps) {
			name = steps[stepIdx].Name
		}
		ctx.Progress <- StepProgress{
			Step: stepIdx, Total: len(steps), Name: name,
			Tool: toolName, Status: Success, Duration: time.Since(start),
			Count: int(fi.Size() / 1024), Unit: "KB",
		}
	} else {
		report(stepIdx, Success, time.Since(start), nil, toolName)
	}
	return output
}

// resolveDotnetSDK returns a path to a `dotnet` executable that has at least one
// SDK installed. On this machine the PATH `dotnet` may be an x86 stub without an
// SDK, so we probe `dotnet --list-sdks` and fall back to the canonical install at
// C:\Program Files\dotnet\dotnet.exe. Returns "" if no SDK is found anywhere.
func (d *DotnetConfuserEx) resolveDotnetSDK(ctx *Context) string {
	candidates := []string{"dotnet"}
	if pf := os.Getenv("ProgramFiles"); pf != "" {
		candidates = append(candidates, filepath.Join(pf, "dotnet", "dotnet.exe"))
	}
	candidates = append(candidates, `C:\Program Files\dotnet\dotnet.exe`)

	seen := map[string]bool{}
	for _, c := range candidates {
		if c != "dotnet" {
			if _, err := os.Stat(c); err != nil {
				continue
			}
		}
		if seen[c] {
			continue
		}
		seen[c] = true
		r, err := util.RunCmd(ctx.Ctx, c, []string{"--list-sdks"}, "")
		if err != nil || r == nil || r.ExitCode != 0 {
			continue
		}
		if strings.TrimSpace(r.Stdout) != "" {
			return c
		}
	}
	return ""
}

// buildExtractor builds (cached) the cfxextract embedded-assembly extractor and
// returns its dll path. Thin wrapper over buildDotnetTool.
func (d *DotnetConfuserEx) buildExtractor(ctx *Context, dotnet string, logTool func(string, string)) (string, error) {
	return buildDotnetTool(ctx, dotnet, logTool,
		"cfxextract", "extract.csproj", "cfxextract.dll", cfxExtractAssets, "assets/cfxextract/",
		[]string{"extract.csproj", "Program.cs"},
		"Building embedded extractor (dotnet build -c Release, first run restores NuGet ~15s)...")
}

// buildStringsDecryptor builds (cached) the cfxstrings custom-string-decryptor
// pass and returns its dll path. Thin wrapper over buildDotnetTool.
func (d *DotnetConfuserEx) buildStringsDecryptor(ctx *Context, dotnet string, logTool func(string, string)) (string, error) {
	return buildDotnetTool(ctx, dotnet, logTool,
		"cfxstrings", "cfxstrings.csproj", "cfxstrings.dll", cfxStringsAssets, "assets/cfxstrings/",
		[]string{"cfxstrings.csproj", "Program.cs"},
		"Building custom string-decryptor pass (dotnet build -c Release, first run restores NuGet ~15s)...")
}

// buildDotnetTool writes an embedded .NET tool's source into a cache dir under
// BaseDir/tools/<tool>/ (if missing), builds it once with `dotnet build -c
// Release`, and returns the path to the built <dllName>. Subsequent runs reuse
// the cached dll. Shared by cfxextract and cfxstrings.
func buildDotnetTool(ctx *Context, dotnet string, logTool func(string, string),
	tool, csproj, dllName string, assets embed.FS, assetPrefix string, srcFiles []string, buildMsg string) (string, error) {
	cacheDir := util.ToolDir(tool)
	binDir := filepath.Join(cacheDir, "bin", "Release", "net8.0")
	dll := filepath.Join(binDir, dllName)

	// Cached build — reuse.
	if _, err := os.Stat(dll); err == nil {
		return dll, nil
	}

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}

	// Write embedded source (overwrite to keep it in sync with the binary).
	for _, name := range srcFiles {
		data, err := assets.ReadFile(assetPrefix + name)
		if err != nil {
			return "", fmt.Errorf("read embedded %s: %w", name, err)
		}
		if err := os.WriteFile(filepath.Join(cacheDir, name), data, 0644); err != nil {
			return "", fmt.Errorf("write %s: %w", name, err)
		}
	}

	logTool(tool, buildMsg)
	r, err := util.RunCmd(ctx.Ctx, dotnet, []string{"build", "-c", "Release", csproj}, cacheDir)
	if err != nil {
		return "", fmt.Errorf("dotnet build: %w", err)
	}
	if r.ExitCode != 0 {
		return "", fmt.Errorf("dotnet build exit %d: %s", r.ExitCode, strings.TrimSpace(r.Stderr+r.Stdout))
	}
	if _, err := os.Stat(dll); err != nil {
		return "", fmt.Errorf("build produced no %s at %s", dllName, dll)
	}
	return dll, nil
}

// extractEmbedded builds (cached) and runs the cfxextract tool against the
// ORIGINAL target, parses its stdout, writes a manifest, and returns the count
// of extracted assemblies plus the full paths of the extracted *.dll files.
func (d *DotnetConfuserEx) extractEmbedded(ctx *Context, logTool func(string, string)) (int, []string, error) {
	dotnet := d.resolveDotnetSDK(ctx)
	if dotnet == "" {
		return 0, nil, fmt.Errorf("no .NET SDK found (checked PATH and C:\\Program Files\\dotnet); install the .NET 8 SDK")
	}

	dll, err := d.buildExtractor(ctx, dotnet, logTool)
	if err != nil {
		return 0, nil, err
	}

	outDir := filepath.Join(ctx.Output, "extracted")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return 0, nil, fmt.Errorf("create output dir: %w", err)
	}

	logTool("cfxextract", "==================== SECURITY WARNING ====================")
	logTool("cfxextract", "Dynamic extraction EXECUTES the target's own code in-process")
	logTool("cfxextract", "(forces its <Module> cctor to decrypt embedded assemblies).")
	logTool("cfxextract", "Only run on binaries you trust. Enabled via --allow-dynamic.")
	logTool("cfxextract", "=========================================================")

	// CRITICAL: pass the ORIGINAL target (ctx.Target), not the de4dot-processed
	// stage — de4dot can break the cctor/anti-tamper the decryptor relies on. The
	// original also resolves its sibling dependencies from its own directory.
	count := 0
	var extracted []string
	r, runErr := util.RunCmdStreaming(ctx.Ctx, dotnet, []string{dll, ctx.Target, outDir}, "", func(line string) {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "EXTRACTED:"):
			rest := strings.TrimPrefix(line, "EXTRACTED:")
			name := rest
			if i := strings.Index(rest, " "); i >= 0 {
				name = rest[:i]
			}
			extracted = append(extracted, name)
			logTool("cfxextract", "Extracted "+name)
		case strings.HasPrefix(line, "EXTRACT_COUNT:"):
			fmt.Sscanf(strings.TrimPrefix(line, "EXTRACT_COUNT:"), "%d", &count)
		case strings.HasPrefix(line, "cctor-warn:") || strings.HasPrefix(line, "parts-warn:") || strings.HasPrefix(line, "APPLICATION_PARTS:"):
			logTool("cfxextract", line)
		}
	})
	if runErr != nil {
		return 0, nil, fmt.Errorf("run extractor: %w", runErr)
	}
	if r != nil && r.ExitCode != 0 {
		return 0, nil, fmt.Errorf("extractor exit %d: %s", r.ExitCode, strings.TrimSpace(r.Stderr))
	}
	if count == 0 {
		count = len(extracted)
	}

	// Resolve each extracted assembly name to its written path under outDir.
	var dllPaths []string
	for _, name := range extracted {
		p := filepath.Join(outDir, name)
		if _, statErr := os.Stat(p); statErr == nil {
			dllPaths = append(dllPaths, p)
		}
	}

	manifest := map[string]any{
		"type":            "confuserex_resources",
		"host_assembly":   filepath.Base(ctx.Target),
		"extracted_count": count,
		"extracted":       extracted,
	}
	if data, err := json.MarshalIndent(manifest, "", "  "); err == nil {
		os.WriteFile(filepath.Join(outDir, "embedded_manifest.json"), data, 0644)
	}

	logTool("cfxextract", fmt.Sprintf("Extracted %d embedded assemblies -> %s", count, outDir))
	return count, dllPaths, nil
}

// decompileExtracted runs the per-child sub-pipeline for each extracted embedded
// assembly: de4dot-cex `-p crx --dont-rename` → cfxstrings custom-string-decryptor
// pass → ilspycmd `-p`, writing output under extracted/<name>/{src,strings.json,
// index.json}. Each child is best-effort: a failure on one child logs and moves
// on rather than aborting the host run. Returns the number of children whose
// decompilation produced source. Reuses the existing tool helpers and the
// cfxstrings static rewrite (no target code execution — safe to run on the
// extracted obfuscated assemblies regardless of --allow-dynamic, though this
// path is only reached when --allow-dynamic produced the extracted assemblies).
func (d *DotnetConfuserEx) decompileExtracted(
	ctx *Context, dlls []string,
	report func(int, StepStatus, time.Duration, error, string),
	logTool func(string, string),
) int {
	ilspyPath, err := ctx.Tools.Resolve("ilspycmd")
	if err != nil {
		logTool("cfxstrings", fmt.Sprintf("ilspycmd not available, cannot decompile extracted assemblies: %v", err))
		return 0
	}

	// Build (cached) the cfxstrings pass once. If it can't build (no SDK), we
	// still decompile children with whatever de4dot produced — the pass is an
	// enhancement, not a hard requirement.
	dotnet := d.resolveDotnetSDK(ctx)
	cfxStringsDLL := ""
	if dotnet == "" {
		logTool("cfxstrings", "no .NET SDK found — skipping custom string-decryptor pass on extracted assemblies")
	} else if built, berr := d.buildStringsDecryptor(ctx, dotnet, logTool); berr != nil {
		logTool("cfxstrings", fmt.Sprintf("custom string-decryptor build failed (%v) — decompiling without it", berr))
	} else {
		cfxStringsDLL = built
	}

	de4dotPath, de4dotErr := ctx.Tools.Resolve("de4dot-cex")

	done := 0
	for _, dll := range dlls {
		name := strings.TrimSuffix(filepath.Base(dll), filepath.Ext(dll))
		childDir := filepath.Join(ctx.Output, "extracted", name)
		childInter := filepath.Join(childDir, "intermediate")
		if err := os.MkdirAll(childInter, 0755); err != nil {
			logTool("cfxstrings", fmt.Sprintf("%s: create dir: %v — skipping", name, err))
			continue
		}
		stage := dll

		// 1. de4dot-cex -p crx --dont-rename (extracted children are ConfuserEx).
		if de4dotErr == nil {
			if _, statErr := os.Stat(de4dotPath); statErr == nil {
				out := filepath.Join(childInter, "de4dot"+filepath.Ext(dll))
				args := []string{stage, "-p", "crx", "--dont-rename", "-o", out}
				if r, rerr := util.RunCmd(ctx.Ctx, de4dotPath, args, ""); rerr == nil && r != nil && r.ExitCode == 0 {
					if _, e := os.Stat(out); e == nil {
						stage = out
						logTool("de4dot-cex", fmt.Sprintf("%s: deobfuscated", name))
					}
				} else {
					logTool("de4dot-cex", fmt.Sprintf("%s: de4dot did not apply — using raw extracted assembly", name))
				}
			}
		}

		// 2. cfxstrings custom-string-decryptor static rewrite. Generic: handles
		// ConfuserEx custom resource-keyed decryptors de4dot -p crx leaves behind.
		//
		// PER-CHILD GATE: extracted children are a MIX — some ConfuserEx-obfuscated,
		// some CLEAN third-party dependency assemblies. Only run cfxstrings on a
		// child that actually has residual encrypted (PUA) user-strings. A clean
		// child has none, so we skip it entirely and pass it through untouched.
		// (cfxstrings has its own XOR-shape + key-resource gates as a second line of
		// defence, but this avoids even invoking it on clean assemblies.)
		childMode, childKeySrc := "", ""
		if cfxStringsDLL != "" {
			pua, _, perr := recon.CountPUAUserStrings(stage)
			if perr != nil {
				logTool("cfxstrings", fmt.Sprintf("%s: PUA pre-check failed (%v) — skipping string-decryptor pass", name, perr))
			} else if pua == 0 {
				logTool("cfxstrings", fmt.Sprintf("%s: no residual encrypted (\\ueXXXX) strings — clean, skipping string-decryptor pass", name))
			} else {
				out := filepath.Join(childInter, "cfxstrings"+filepath.Ext(dll))
				tsv := filepath.Join(childDir, "decrypted_strings.tsv")
				rewrote, residual := 0, 0
				// The recursion path is only reachable when the host run had
				// --allow-dynamic (it produced the extracted assemblies), so the
				// cfxstrings dynamic-invoke fallback is permitted here too. The static
				// XOR path is primary; dynamic only kicks in for sites it can't resolve.
				cfxArgs := []string{cfxStringsDLL, stage, out, tsv}
				if ctx.AllowDynamic {
					cfxArgs = append(cfxArgs, "--allow-dynamic")
				}
				r, rerr := util.RunCmdStreaming(ctx.Ctx, dotnet, cfxArgs, "", func(line string) {
					line = strings.TrimSpace(line)
					switch {
					case strings.HasPrefix(line, "REWROTE:"):
						fmt.Sscanf(strings.TrimPrefix(line, "REWROTE:"), "%d", &rewrote)
					case strings.HasPrefix(line, "RESIDUAL:"):
						fmt.Sscanf(strings.TrimPrefix(line, "RESIDUAL:"), "%d", &residual)
					case strings.HasPrefix(line, "MODE:"):
						childMode = strings.TrimPrefix(line, "MODE:")
						logTool("cfxstrings", name+": "+line)
					case strings.HasPrefix(line, "KEY:"):
						// KEY:<source> ... — source is "resource:<n>" or "recovered"
						f := strings.Fields(strings.TrimPrefix(line, "KEY:"))
						if len(f) > 0 {
							childKeySrc = f[0]
						}
						logTool("cfxstrings", name+": "+line)
					case strings.HasPrefix(line, "DECRYPTOR:") || strings.HasPrefix(line, "SHAPE:") || strings.HasPrefix(line, "KEYRES:") || strings.HasPrefix(line, "KEYNOTE:"):
						logTool("cfxstrings", name+": "+line)
					}
				})
				if rerr == nil && r != nil && r.ExitCode == 0 {
					if _, e := os.Stat(out); e == nil {
						stage = out
						switch {
						case residual > 0:
							logTool("cfxstrings", fmt.Sprintf("%s: rewrote %d encrypted strings, %d left encrypted (unrecoverable)", name, rewrote, residual))
						case rewrote > 0:
							logTool("cfxstrings", fmt.Sprintf("%s: rewrote %d encrypted strings", name, rewrote))
						}
					}
				} else {
					stderr := ""
					if r != nil {
						stderr = strings.TrimSpace(r.Stderr)
					}
					logTool("cfxstrings", fmt.Sprintf("%s: string-decryptor pass skipped: %v %s", name, rerr, stderr))
				}
			}
		}

		// 3. ilspycmd -p → extracted/<name>/src
		childSrc := filepath.Join(childDir, "src")
		if err := os.MkdirAll(childSrc, 0755); err != nil {
			logTool("ilspycmd", fmt.Sprintf("%s: create src dir: %v — skipping", name, err))
			continue
		}
		ilspyArgs := []string{"-p", "-o", childSrc, stage}
		if ctx.Config.CSharpLanguageVersion != "Auto" && ctx.Config.CSharpLanguageVersion != "" {
			ilspyArgs = append(ilspyArgs, "--languageversion", ctx.Config.CSharpLanguageVersion)
		}
		ilspyBin, ilspyRun := dotnetExec(ctx.Ctx, ilspyPath, ilspyArgs)
		r, runErr := util.RunCmd(ctx.Ctx, ilspyBin, ilspyRun, "")
		if runErr != nil || r == nil || r.ExitCode != 0 {
			stderr := ""
			if r != nil {
				stderr = r.Stderr
			}
			logTool("ilspycmd", fmt.Sprintf("%s: decompile failed: %v %s", name, runErr, strings.TrimSpace(stderr)))
			continue
		}

		// 4. Per-child strings.json + index.json (reuse existing helpers).
		analyzeChildStrings(childSrc, filepath.Join(childDir, "strings.json"))
		if _, ierr := buildIndex(childSrc); ierr != nil {
			logTool("ilspycmd", fmt.Sprintf("%s: index build failed: %v", name, ierr))
		}

		// Residual PUA in the decompiled source is a necessary (not sufficient)
		// signal: 0 PUA means no \ueXXXX markers remain, but a wrong-but-printable
		// recovered key could still have produced plausible garbage. So we report
		// "no residual PUA markers" (not "clean") and qualify it with the key
		// source — a recovered key is lower-confidence than an authoritative
		// resource key. cfxstrings already withholds rewrites below its confidence
		// bar (leaving those as RESIDUAL), so what reaches here cleared that bar.
		if pua := countPUAInSource(childSrc); pua > 0 {
			logTool("ilspycmd", fmt.Sprintf("%s: WARNING %d residual encrypted (\\ueXXXX) chars remain in source", name, pua))
		} else if strings.HasPrefix(childMode, "static-recovered") || strings.HasPrefix(childKeySrc, "recovered") {
			logTool("ilspycmd", fmt.Sprintf("%s: no residual PUA markers (strings via RECOVERED key — verified above confidence bar, but lower-confidence than a resource key)", name))
		} else if childMode != "" {
			logTool("ilspycmd", fmt.Sprintf("%s: no residual PUA markers (strings via %s)", name, childMode))
		} else {
			logTool("ilspycmd", fmt.Sprintf("%s: no residual PUA markers", name))
		}
		done++
	}
	return done
}

// analyzeChildStrings extracts string literals from a decompiled child's .cs
// source and runs the shared strings categorizer over them. The host path uses
// the `strings` tool on the binary; for extracted children we already have
// readable decompiled source, so we harvest C# string literals directly (the
// decrypted plaintext now lives there) into a temp file and reuse analyzeStrings.
func analyzeChildStrings(srcDir, outJSON string) {
	var sb strings.Builder
	filepath.WalkDir(srcDir, func(path string, de os.DirEntry, err error) error {
		if err != nil || de.IsDir() || filepath.Ext(path) != ".cs" {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		for _, m := range reCSharpStringLit.FindAllStringSubmatch(string(data), -1) {
			sb.WriteString(m[1])
			sb.WriteByte('\n')
		}
		return nil
	})
	tmp := outJSON + ".strings.txt"
	if err := os.WriteFile(tmp, []byte(sb.String()), 0644); err != nil {
		return
	}
	defer os.Remove(tmp)
	analyzeStrings(tmp, outJSON)
}
