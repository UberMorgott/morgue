package recipe

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/UberMorgott/morgue/internal/tools"
	"github.com/UberMorgott/morgue/internal/util"
)

// inspectorSupports reports whether Il2CppInspectorRedux handles a metadata
// version. Ranges per the InspectorRedux CLI: 16–24, 27–29, 31, 35, 38, 39,
// 104–106. (Minor sub-versions like 24.5/29.1 share the integer major.)
func inspectorSupports(v int32) bool {
	switch {
	case v >= 16 && v <= 24:
		return true
	case v >= 27 && v <= 29:
		return true
	case v == 31 || v == 35 || v == 38 || v == 39:
		return true
	case v >= 104 && v <= 106:
		return true
	}
	return false
}

// dumperOrder returns the dumper tool names to try, in order, for a metadata
// version. Default policy: InspectorRedux first when available AND it supports
// the version; legacy Il2CppDumper as fallback. When InspectorRedux can't
// handle the version, try the legacy dumper first (it covers some older sets),
// then InspectorRedux as a last resort if installed.
func dumperOrder(version int32, inspectorAvailable bool) []string {
	if !inspectorAvailable {
		return []string{"il2cppdumper"}
	}
	if inspectorSupports(version) {
		return []string{"il2cppinspector", "il2cppdumper"}
	}
	return []string{"il2cppdumper", "il2cppinspector"}
}

// inspectorArgs builds the InspectorRedux 2026.x CLI argument list. The CLI uses a
// `process` subcommand with positional input paths (binary + metadata, auto-detected
// in any order) and a single output FOLDER (-o). `-s` (--output-csharp-stub) and
// `-d` (--output-dummy-dlls) are bare switches that select the C# stub + DummyDLL
// outputs only; nothing else (no Python/C++/JSON) is emitted. InspectorRedux writes
// the C# to <dumpRoot>/cs/il2cpp.cs and the DummyDLLs to <dumpRoot>/dll/*.dll, which
// is exactly the structured dump/cs + dump/dll layout, so dumpRoot is <out>/dump.
func inspectorArgs(binPath, metadataPath, dumpRoot string) []string {
	return []string{
		"process",
		binPath,
		metadataPath,
		"-o", dumpRoot,
		"-s",
		"-d",
	}
}

// inspectorEnv builds env overrides for the InspectorRedux spawn: DOTNET_ROOT
// points at the ASP.NET runtime; TEMP/TMP go to the output disk so no large temp
// lands on C:. DOTNET_ROLL_FORWARD lets net10.0 resolve to the installed runtime.
func inspectorEnv(aspNetRuntimeDir, tmpDir string) []string {
	return []string{
		"DOTNET_ROOT=" + aspNetRuntimeDir,
		"DOTNET_ROLL_FORWARD=LatestMajor",
		"TEMP=" + tmpDir,
		"TMP=" + tmpDir,
	}
}

// runInspector spawns the InspectorRedux CLI streaming its output to onLine.
//
// It launches with CREATE_BREAKAWAY_FROM_JOB so the process escapes morgue's
// per-process Job Object memory cap. Building the full .NET type model for a large
// IL2CPP image (LC2's GameAssembly.dll is ~165 MB → 192 DummyDLLs) peaks at ~4.5 GB
// working set, above the default 4 GiB cap; under the cap InspectorRedux is
// throttled mid-generation and silently emits only a handful of assemblies (it
// still returns exit 0), so the breakaway is required for a complete dump. This
// mirrors the Ghidra JVM, which breaks away for the same reason.
func runInspector(ctx context.Context, exePath string, args, env []string, workDir string, onLine func(string)) (*util.CmdResult, error) {
	return util.RunCmdStreamingEnvBreakaway(ctx, env, exePath, args, workDir, onLine)
}

// aspNetRuntimeDir resolves the .NET 10 ASP.NET runtime root directory used for
// DOTNET_ROOT. RuntimePath returns the dotnet(.exe) binary; DOTNET_ROOT must be
// the directory that contains it, so we take the parent of the binary path.
func aspNetRuntimeDir(m *tools.Manager) (string, error) {
	bin, err := m.RuntimePath(tools.RuntimeAspNet)
	if err != nil {
		return "", err
	}
	return filepath.Dir(bin), nil
}

// runDumpWithOrder tries each dumper tool in order, returning the first that
// succeeds. If all fail it returns an aggregate error naming every tool tried and
// its error (never silent). This is the pure ordering primitive — runDumpStage
// supplies the real per-tool runner; unit tests inject a fake one.
func runDumpWithOrder(ctx context.Context, order []string, run func(ctx context.Context, tool string) error) (string, error) {
	var errs []string
	for _, tool := range order {
		if err := run(ctx, tool); err != nil {
			errs = append(errs, tool+": "+err.Error())
			continue
		}
		return tool, nil
	}
	return "", fmt.Errorf("all dumpers failed [%s]", strings.Join(errs, " | "))
}

// runDumpStage performs the IL2CPP dump (Step 1) version-aware with fallback.
// Policy (dumperOrder): Il2CppInspectorRedux first when installed AND it supports
// the detected metadata version; legacy Il2CppDumper as the fallback (and as the
// first choice for versions InspectorRedux can't handle). Both tools populate the
// same dummyDllDir (the structured dump/dll dir) so downstream steps are unchanged.
//
// On success it returns the tool name that produced output. If every candidate
// tool is missing or fails, it returns an aggregated error naming the detected
// version, every tool tried, and each tool's error — never a silent failure.
func runDumpStage(ctx *Context, metadataPath, metaDir, dummyDllDir string, version int32, logTool func(tool, msg string)) (string, error) {
	_, inspectorErr := ctx.Tools.Resolve("il2cppinspector")
	inspectorAvailable := inspectorErr == nil

	order := dumperOrder(version, inspectorAvailable)

	// runOne executes a single dumper and verifies it produced assemblies. The
	// success criterion is output presence (Il2CppDumper crashes on a trailing
	// Console.ReadKey() despite finishing its work, so the exit code is unreliable).
	runOne := func(c context.Context, tool string) error {
		var runErr error
		switch tool {
		case "il2cppinspector":
			//nolint:contextcheck // reaches aspNetRuntimeDir -> Manager.RuntimePath ->
			// systemDotNetHasAspNet10, a local `dotnet --list-runtimes` probe with its own
			// 10s timeout; the dump itself does run under ctx.Ctx.
			runErr = runInspectorDump(ctx, metadataPath, dummyDllDir, logTool)
		case "il2cppdumper":
			runErr = runLegacyDump(ctx, metadataPath, metaDir, dummyDllDir, logTool)
		default:
			return fmt.Errorf("unknown dumper %q", tool)
		}
		if runErr != nil {
			logTool(tool, fmt.Sprintf("dump attempt failed, trying next: %v", runErr))
			return runErr
		}
		if _, statErr := os.Stat(dummyDllDir); statErr != nil || countFiles(dummyDllDir, ".dll") == 0 {
			err := fmt.Errorf("%s produced no assemblies in %s", tool, dummyDllDir)
			logTool(tool, fmt.Sprintf("dump attempt failed, trying next: %v", err))
			return err
		}
		return nil
	}

	used, err := runDumpWithOrder(ctx.Ctx, order, runOne)
	if err != nil {
		return "", fmt.Errorf(
			"no IL2CPP dumper succeeded for metadata version %d; tried [%s]: %w",
			version, strings.Join(order, ", "), err,
		)
	}
	return used, nil
}

// runInspectorDump resolves and runs Il2CppInspectorRedux, writing DummyDLLs into
// dummyDllDir (so downstream ilspycmd input is identical to the legacy path) and
// the merged C# into <dump/cs>/il2cpp.cs. It points DOTNET_ROOT at the ASP.NET
// runtime and redirects TEMP/TMP onto the output tree.
func runInspectorDump(ctx *Context, metadataPath, dummyDllDir string, logTool func(tool, msg string)) error {
	exePath, err := ctx.Tools.Resolve("il2cppinspector")
	if err != nil {
		return fmt.Errorf("il2cppinspector not available: %w", err)
	}
	runtimeDir, err := aspNetRuntimeDir(ctx.Tools)
	if err != nil {
		return fmt.Errorf("ASP.NET runtime unavailable for InspectorRedux: %w", err)
	}

	// InspectorRedux 2026.x writes cs/ and dll/ subdirs under a single output
	// folder. dummyDllDir is <out>/dump/dll, so the dump root is its parent and
	// the C# lands at <dumpRoot>/cs/il2cpp.cs — matching the structured layout.
	dumpRoot := filepath.Dir(dummyDllDir)
	csOutDir := filepath.Join(dumpRoot, "cs")
	tmpDir := filepath.Join(ctx.Output, ".tmp")
	for _, d := range []string{dumpRoot, dummyDllDir, csOutDir, tmpDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}

	args := inspectorArgs(ctx.Target, metadataPath, dumpRoot)
	env := inspectorEnv(runtimeDir, tmpDir)

	logTool("il2cppinspector", fmt.Sprintf("Running Il2CppInspectorRedux: %s + %s", filepath.Base(ctx.Target), filepath.Base(metadataPath)))

	// InspectorRedux's CLI starts a SignalR/Kestrel host that does NOT shut down
	// after the `process` command finishes — the process lingers indefinitely.
	// When it prints "Export finished" every output (cs/il2cpp.cs + dll/*.dll) is
	// already on disk, so we cancel its context to terminate the idle host and
	// unblock the wait. A cancel-induced kill after that marker is the expected,
	// successful path. The streaming callback runs in the same goroutine as the
	// read loop, so exportDone needs no synchronization.
	runCtx, cancelRun := context.WithCancel(ctx.Ctx)
	defer cancelRun()
	exportDone := false
	lastLog := time.Now().Add(-2 * time.Second)
	result, runErr := runInspector(runCtx, exePath, args, env, ctx.Output, func(line string) {
		if strings.Contains(line, "Export finished") {
			exportDone = true
			cancelRun()
		}
		if time.Since(lastLog) >= time.Second && strings.TrimSpace(line) != "" {
			logTool("il2cppinspector", strings.TrimSpace(line))
			lastLog = time.Now()
		}
	})
	// Success path: we saw "Export finished"; the kill we triggered is expected,
	// so ignore the resulting context-cancelled / non-zero exit.
	if exportDone {
		return nil
	}
	if runErr != nil {
		return fmt.Errorf("InspectorRedux spawn failed: %w", runErr)
	}
	if result != nil && result.ExitCode != 0 {
		stderr := strings.TrimSpace(result.Stderr)
		return fmt.Errorf("InspectorRedux exited %d: %s", result.ExitCode, stderr)
	}
	return nil
}

// runLegacyDump runs the original Il2CppDumper path, preserved as the fallback.
// Il2CppDumper writes a DummyDll/ subdirectory under metaDir; that subdirectory is
// dummyDllDir, so the caller's success check and downstream steps are unchanged.
func runLegacyDump(ctx *Context, metadataPath, metaDir, dummyDllDir string, logTool func(tool, msg string)) error {
	dumperPath, err := ctx.Tools.Resolve("il2cppdumper")
	if err != nil {
		return fmt.Errorf("il2cppdumper not available: %w", err)
	}
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		return err
	}

	logTool("il2cppdumper", fmt.Sprintf("Running Il2CppDumper: %s + %s", filepath.Base(ctx.Target), filepath.Base(metadataPath)))
	lastLog := time.Now().Add(-2 * time.Second)
	result, runErr := util.RunCmdStreamingWithStdin(ctx.Ctx, dumperPath, []string{
		ctx.Target,
		metadataPath,
		metaDir,
	}, "", strings.NewReader("\r\n"), func(line string) {
		if time.Since(lastLog) >= time.Second && strings.TrimSpace(line) != "" {
			logTool("il2cppdumper", fmt.Sprintf("Il2CppDumper: %s", strings.TrimSpace(line)))
			lastLog = time.Now()
		}
	})

	// Il2CppDumper crashes on Console.ReadKey() after completing work — defer the
	// real success decision to the caller's DummyDll output check. Only surface a
	// spawn-level error (binary missing, killed) plus any captured stderr here.
	if runErr != nil {
		stderr := ""
		if result != nil {
			stderr = strings.TrimSpace(result.Stderr)
		}
		return fmt.Errorf("Il2CppDumper spawn failed: %w (%s)", runErr, stderr)
	}

	// Il2CppDumper always emits its assemblies into a fixed "DummyDll" subdir of
	// metaDir. When the caller's shared dummyDllDir is that exact subdir the dump
	// already lands there; otherwise (the structured dump/dll layout) mirror the
	// produced DLLs into dummyDllDir so the success check + downstream ilspycmd see
	// them in one place.
	producedDir := filepath.Join(metaDir, "DummyDll")
	if filepath.Clean(producedDir) != filepath.Clean(dummyDllDir) {
		if _, statErr := os.Stat(producedDir); statErr == nil {
			if cpErr := copyDirFlat(producedDir, dummyDllDir); cpErr != nil {
				return fmt.Errorf("mirror Il2CppDumper output into %s: %w", dummyDllDir, cpErr)
			}
		}
	}
	return nil
}
