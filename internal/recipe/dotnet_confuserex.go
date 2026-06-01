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

// DotnetConfuserEx handles ConfuserEx-obfuscated .NET assemblies.
type DotnetConfuserEx struct{}

func init() {
	RegisterFirst(&DotnetConfuserEx{})
}

func (d *DotnetConfuserEx) Name() string        { return "dotnet-confuserex" }
func (d *DotnetConfuserEx) Description() string { return "Deobfuscate ConfuserEx-protected .NET assembly" }

func (d *DotnetConfuserEx) Match(r *recon.Result) bool {
	if r.Kind != recon.Managed {
		return false
	}
	lower := strings.ToLower(r.Obfuscator)
	return strings.Contains(lower, "confuserex") || strings.Contains(lower, "confuser")
}

func (d *DotnetConfuserEx) Steps() []StepInfo {
	return []StepInfo{
		{Name: "Copy original", Required: false},
		{Name: "NoFuserEx fast-path", Required: false},
		{Name: "Unpack", Required: false},
		{Name: "String decrypt", Required: false},
		{Name: "Control flow + rename", Required: false},
		{Name: "Proxy removal", Required: false},
		{Name: "Extract strings", Required: false},
		{Name: "Extract embedded", Required: false},
		{Name: "Decompile", Required: true},
		{Name: "Build indexes", Required: false},
	}
}

func (d *DotnetConfuserEx) RequiredTools() []string {
	return []string{"ilspycmd", "strings", "de4dot-cex", "nofuserex", "confuserex-killer", "proxycall-remover"}
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
	os.MkdirAll(interDir, 0755)

	// current tracks the working copy through the pipeline
	current := ctx.Target

	// Step 0: Copy original. Always persisted (a single copy of the target is
	// cheap and valuable for reproducibility) — kept consistent across recipes.
	var start time.Time
	report(0, Running, 0, nil, "")
	start = time.Now()
	origDir := filepath.Join(ctx.Output, "original")
	os.MkdirAll(origDir, 0755)
	origCopy := filepath.Join(origDir, filepath.Base(ctx.Target))
	if err := copyFile(ctx.Target, origCopy); err != nil {
		report(0, Failed, time.Since(start), err, "")
		return err
	}
	report(0, Success, time.Since(start), nil, "")

	// Step 1: NoFuserEx fast-path (anti-tamper removal)
	current = d.runToolStep(ctx, 1, current, interDir, "nofuserex", func(toolPath, input, output string) error {
		r, err := util.RunCmd(ctx.Ctx, toolPath, []string{input, "-o", output}, "")
		if err != nil {
			return err
		}
		if r.ExitCode != 0 {
			return fmt.Errorf("nofuserex exit %d: %s", r.ExitCode, r.Stderr)
		}
		return nil
	}, report, logTool)

	// Step 2: Unpack (resource/constant unpacking)
	current = d.runToolStep(ctx, 2, current, interDir, "confuserex-killer", func(toolPath, input, output string) error {
		r, err := util.RunCmd(ctx.Ctx, toolPath, []string{input, "-o", output}, "")
		if err != nil {
			return err
		}
		if r.ExitCode != 0 {
			return fmt.Errorf("unpacker exit %d: %s", r.ExitCode, r.Stderr)
		}
		return nil
	}, report, logTool)

	// Step 3: String decrypt (de4dot-cex --strtyp delegate)
	current = d.runToolStep(ctx, 3, current, interDir, "de4dot-cex", func(toolPath, input, output string) error {
		r, err := util.RunCmd(ctx.Ctx, toolPath, []string{input, "--strtyp", "delegate", "-o", output}, "")
		if err != nil {
			return err
		}
		if r.ExitCode != 0 {
			return fmt.Errorf("de4dot-cex exit %d: %s", r.ExitCode, r.Stderr)
		}
		return nil
	}, report, logTool)

	// Step 4: Control flow + rename (de4dot-cex second pass)
	current = d.runToolStep(ctx, 4, current, interDir, "de4dot-cex", func(toolPath, input, output string) error {
		r, err := util.RunCmd(ctx.Ctx, toolPath, []string{input, "--un-name", "!^<", "-o", output}, "")
		if err != nil {
			return err
		}
		if r.ExitCode != 0 {
			return fmt.Errorf("de4dot-cex exit %d: %s", r.ExitCode, r.Stderr)
		}
		return nil
	}, report, logTool)

	// Step 5: Proxy removal
	current = d.runToolStep(ctx, 5, current, interDir, "proxycall-remover", func(toolPath, input, output string) error {
		r, err := util.RunCmd(ctx.Ctx, toolPath, []string{input, "-o", output}, "")
		if err != nil {
			return err
		}
		if r.ExitCode != 0 {
			return fmt.Errorf("proxycall-remover exit %d: %s", r.ExitCode, r.Stderr)
		}
		return nil
	}, report, logTool)

	// Step 6: Extract strings
	report(6, Running, 0, nil, "strings")
	start = time.Now()
	stringsPath, err := ctx.Tools.Resolve("strings")
	if err != nil {
		logTool("strings", fmt.Sprintf("strings tool not available: %v", err))
		report(6, Skipped, time.Since(start), nil, "strings")
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
						Step: 6, Total: total, Name: steps[6].Name,
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
		reportCount(6, time.Since(start), "strings", strCount, "strings")
	}

	// Step 7: Extract embedded ConfuserEx resources.
	//
	// The embedded assemblies live in a custom-encrypted ConfuserEx resource and
	// can only be decrypted by the target's own <Module> cctor at runtime. We load
	// the ORIGINAL target in-process (ctx.Target, NOT the de4dot-processed stage,
	// which can break anti-tamper/cctor), force its module cctor, and capture the
	// decrypted bytes via a Harmony prefix on Assembly.Load. This EXECUTES TARGET
	// CODE, so it is gated behind --allow-dynamic.
	report(7, Running, 0, nil, "cfxextract")
	start = time.Now()
	if !ctx.AllowDynamic {
		logTool("cfxextract", "embedded extraction requires --allow-dynamic (executes target code); skipping")
		report(7, Skipped, time.Since(start), nil, "cfxextract")
	} else {
		count, err := d.extractEmbedded(ctx, logTool)
		if err != nil {
			logTool("cfxextract", fmt.Sprintf("embedded extraction skipped: %v", err))
			report(7, Skipped, time.Since(start), nil, "cfxextract")
		} else {
			reportCount(7, time.Since(start), "cfxextract", count, "assemblies")
		}
	}

	// Step 8: Decompile
	report(8, Running, 0, nil, "ilspycmd")
	start = time.Now()
	ilspyPath, err := ctx.Tools.Resolve("ilspycmd")
	if err != nil {
		report(8, Failed, time.Since(start), err, "ilspycmd")
		return fmt.Errorf("ilspycmd not available: %w", err)
	}
	srcDir := filepath.Join(ctx.Output, "src")
	os.MkdirAll(srcDir, 0755)
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
		report(8, Failed, time.Since(start), execErr, "ilspycmd")
		return execErr
	}
	csCount := countFilesWithExt(srcDir, ".cs")
	reportCount(8, time.Since(start), "ilspycmd", csCount, "types")

	// Step 9: Build indexes
	report(9, Running, 0, nil, "")
	start = time.Now()
	logTool("ilspycmd", "Building indexes for decompiled output")
	if _, statErr := os.Stat(srcDir); statErr != nil {
		logTool("ilspycmd", "No source to index — ilspycmd produced no src/ output, skipping")
		report(9, Skipped, time.Since(start), nil, "")
	} else if idx, err := buildIndex(srcDir); err != nil {
		logTool("ilspycmd", fmt.Sprintf("Build indexes failed: %v", err))
		report(9, Failed, time.Since(start), err, "")
	} else {
		logTool("ilspycmd", fmt.Sprintf("Indexed %d source files (%d bytes) -> index.json", idx.FileCount, idx.TotalBytes))
		reportCount(9, time.Since(start), "", idx.FileCount, "files")
	}

	return nil
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

// buildExtractor writes the embedded cfxextract source into a cache dir under
// BaseDir/tools/cfxextract/ (if missing), builds it once with `dotnet build -c
// Release`, and returns the path to the built cfxextract.dll. Subsequent runs
// reuse the cached dll.
func (d *DotnetConfuserEx) buildExtractor(ctx *Context, dotnet string, logTool func(string, string)) (string, error) {
	cacheDir := util.ToolDir("cfxextract")
	binDir := filepath.Join(cacheDir, "bin", "Release", "net8.0")
	dll := filepath.Join(binDir, "cfxextract.dll")

	// Cached build — reuse.
	if _, err := os.Stat(dll); err == nil {
		return dll, nil
	}

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}

	// Write embedded source (overwrite to keep it in sync with the binary).
	for _, name := range []string{"extract.csproj", "Program.cs"} {
		data, err := cfxExtractAssets.ReadFile("assets/cfxextract/" + name)
		if err != nil {
			return "", fmt.Errorf("read embedded %s: %w", name, err)
		}
		if err := os.WriteFile(filepath.Join(cacheDir, name), data, 0644); err != nil {
			return "", fmt.Errorf("write %s: %w", name, err)
		}
	}

	logTool("cfxextract", "Building embedded extractor (dotnet build -c Release, first run restores NuGet ~15s)...")
	r, err := util.RunCmd(ctx.Ctx, dotnet, []string{"build", "-c", "Release", "extract.csproj"}, cacheDir)
	if err != nil {
		return "", fmt.Errorf("dotnet build: %w", err)
	}
	if r.ExitCode != 0 {
		return "", fmt.Errorf("dotnet build exit %d: %s", r.ExitCode, strings.TrimSpace(r.Stderr+r.Stdout))
	}
	if _, err := os.Stat(dll); err != nil {
		return "", fmt.Errorf("build produced no cfxextract.dll at %s", dll)
	}
	return dll, nil
}

// extractEmbedded builds (cached) and runs the cfxextract tool against the
// ORIGINAL target, parses its stdout, writes a manifest, and returns the count
// of extracted assemblies.
func (d *DotnetConfuserEx) extractEmbedded(ctx *Context, logTool func(string, string)) (int, error) {
	dotnet := d.resolveDotnetSDK(ctx)
	if dotnet == "" {
		return 0, fmt.Errorf("no .NET SDK found (checked PATH and C:\\Program Files\\dotnet); install the .NET 8 SDK")
	}

	dll, err := d.buildExtractor(ctx, dotnet, logTool)
	if err != nil {
		return 0, err
	}

	outDir := filepath.Join(ctx.Output, "extracted")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return 0, fmt.Errorf("create output dir: %w", err)
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
		return 0, fmt.Errorf("run extractor: %w", runErr)
	}
	if r != nil && r.ExitCode != 0 {
		return 0, fmt.Errorf("extractor exit %d: %s", r.ExitCode, strings.TrimSpace(r.Stderr))
	}
	if count == 0 {
		count = len(extracted)
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
	return count, nil
}
