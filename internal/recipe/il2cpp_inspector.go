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

// inspectorArgs builds the InspectorRedux CLI argument list. --select-outputs
// restricts emission to the C# file (-c) and DLLs (-d) only (no Python/C++/JSON).
// The C# output path is normalized to forward slashes; InspectorRedux accepts
// either separator on Windows.
func inspectorArgs(binPath, metadataPath, csOutDir, dllOutDir string) []string {
	return []string{
		"-i", binPath,
		"-m", metadataPath,
		"--select-outputs",
		"-c", filepath.ToSlash(filepath.Join(csOutDir, "il2cpp.cs")),
		"-d", dllOutDir,
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
func runInspector(ctx context.Context, exePath string, args, env []string, workDir string, onLine func(string)) (*util.CmdResult, error) {
	return util.RunCmdStreamingEnv(ctx, env, exePath, args, workDir, onLine)
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

// runDumpStage performs the IL2CPP dump (Step 1) version-aware with fallback.
// Policy (dumperOrder): Il2CppInspectorRedux first when installed AND it supports
// the detected metadata version; legacy Il2CppDumper as the fallback (and as the
// first choice for versions InspectorRedux can't handle). Both tools populate the
// same dummyDllDir so downstream steps are unchanged.
//
// On success it returns the tool name that produced output. If every candidate
// tool is missing or fails, it returns an aggregated error naming the detected
// version, every tool tried, and each tool's error — never a silent failure.
func runDumpStage(ctx *Context, metadataPath, metaDir, dummyDllDir string, version int32, logTool func(tool, msg string)) (string, error) {
	_, inspectorErr := ctx.Tools.Resolve("il2cppinspector")
	inspectorAvailable := inspectorErr == nil

	order := dumperOrder(version, inspectorAvailable)
	var attempts []string

	for _, tool := range order {
		var runErr error
		switch tool {
		case "il2cppinspector":
			runErr = runInspectorDump(ctx, metadataPath, dummyDllDir, logTool)
		case "il2cppdumper":
			runErr = runLegacyDump(ctx, metadataPath, metaDir, dummyDllDir, logTool)
		default:
			continue
		}
		// Success criterion (shared with the legacy path): DummyDll dir exists and
		// holds at least one assembly. Il2CppDumper crashes on a trailing
		// Console.ReadKey() despite finishing its work, so output presence — not the
		// exit code — is the gate.
		if runErr == nil {
			if _, statErr := os.Stat(dummyDllDir); statErr == nil && countFiles(dummyDllDir, ".dll") > 0 {
				return tool, nil
			}
			runErr = fmt.Errorf("%s produced no assemblies in %s", tool, dummyDllDir)
		}
		attempts = append(attempts, fmt.Sprintf("%s: %v", tool, runErr))
		logTool(tool, fmt.Sprintf("dump attempt failed, trying next: %v", runErr))
	}

	return "", fmt.Errorf(
		"no IL2CPP dumper succeeded for metadata version %d; tried [%s]: %s",
		version, strings.Join(order, ", "), strings.Join(attempts, " | "),
	)
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

	csOutDir := filepath.Join(ctx.Output, "dump", "cs")
	tmpDir := filepath.Join(ctx.Output, ".tmp")
	for _, d := range []string{dummyDllDir, csOutDir, tmpDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}

	args := inspectorArgs(ctx.Target, metadataPath, csOutDir, dummyDllDir)
	env := inspectorEnv(runtimeDir, tmpDir)

	logTool("il2cppinspector", fmt.Sprintf("Running Il2CppInspectorRedux: %s + %s", filepath.Base(ctx.Target), filepath.Base(metadataPath)))
	lastLog := time.Now().Add(-2 * time.Second)
	result, runErr := runInspector(ctx.Ctx, exePath, args, env, ctx.Output, func(line string) {
		if time.Since(lastLog) >= time.Second && strings.TrimSpace(line) != "" {
			logTool("il2cppinspector", strings.TrimSpace(line))
			lastLog = time.Now()
		}
	})
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
	return nil
}
