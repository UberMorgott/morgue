package util

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// CmdResult holds the output of a command execution.
type CmdResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}

// RunCmd executes a command with context support for timeouts.
// If dir is empty, the current working directory is used.
func RunCmd(ctx context.Context, name string, args []string, dir string) (*CmdResult, error) {
	return RunCmdWithEnv(ctx, name, args, dir, nil)
}

// RunCmdWithStdin executes a command with custom stdin.
// Useful for tools like Il2CppDumper that call Console.ReadKey() and crash
// without a console — passing a newline lets them exit gracefully.
func RunCmdWithStdin(ctx context.Context, name string, args []string, dir string, stdin io.Reader) (*CmdResult, error) {
	return runCmd(ctx, name, args, dir, nil, stdin)
}

// RunCmdWithEnv executes a command with custom environment variables.
// env is a list of "KEY=VALUE" strings that are appended to the current environment.
// If env is nil, the default environment is used.
func RunCmdWithEnv(ctx context.Context, name string, args []string, dir string, env []string) (*CmdResult, error) {
	return runCmd(ctx, name, args, dir, env, nil)
}

// RunCmdStreaming runs a command and calls onLine for each stdout line in real-time.
// stderr is still buffered. Returns the full CmdResult after completion.
func RunCmdStreaming(ctx context.Context, name string, args []string, dir string, onLine func(line string)) (*CmdResult, error) {
	return runCmdStreaming(ctx, name, args, dir, nil, nil, false, onLine)
}

// RunCmdStreamingWithStdin is like RunCmdStreaming but with custom stdin.
func RunCmdStreamingWithStdin(ctx context.Context, name string, args []string, dir string, stdin io.Reader, onLine func(line string)) (*CmdResult, error) {
	return runCmdStreaming(ctx, name, args, dir, nil, stdin, false, onLine)
}

// RunCmdStreamingEnv is like RunCmdStreaming but with extra environment variables.
// env is a list of "KEY=VALUE" strings appended to the current environment
// (os.Environ()); later entries override earlier ones for the same key.
func RunCmdStreamingEnv(ctx context.Context, env []string, name string, args []string, dir string, onLine func(line string)) (*CmdResult, error) {
	return runCmdStreaming(ctx, name, args, dir, env, nil, false, onLine)
}

// RunCmdStreamingEnvBreakaway is like RunCmdStreamingEnv but launches the process
// with CREATE_BREAKAWAY_FROM_JOB (Windows only) so it and its descendants escape
// morgue's Job Object memory cap. Use this ONLY for tools that legitimately need
// more than the per-process cap — currently just Ghidra's JVM, which sizes its
// heap to a large fraction of physical RAM. On non-Windows the flag is a no-op.
func RunCmdStreamingEnvBreakaway(ctx context.Context, env []string, name string, args []string, dir string, onLine func(line string)) (*CmdResult, error) {
	return runCmdStreaming(ctx, name, args, dir, env, nil, true, onLine)
}

func runCmdStreaming(ctx context.Context, name string, args []string, dir string, env []string, stdin io.Reader, breakaway bool, onLine func(line string)) (*CmdResult, error) {
	if strings.HasSuffix(name, ".dll") {
		args = append([]string{name}, args...)
		localDotnet := filepath.Join(BaseDir(), "runtimes", "dotnet", "dotnet.exe")
		x64Dotnet := filepath.Join(os.Getenv("ProgramW6432"), "dotnet", "dotnet.exe")
		if _, err := os.Stat(localDotnet); err == nil {
			name = localDotnet
		} else if _, err := os.Stat(x64Dotnet); err == nil {
			name = x64Dotnet
		} else {
			name = "dotnet"
		}
		env = append(env, "DOTNET_ROLL_FORWARD=LatestMajor")
	}

	start := time.Now()

	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	if stdin != nil {
		cmd.Stdin = stdin
	}
	HideCmdWindow(cmd)
	if breakaway {
		applyBreakaway(cmd)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe for %s: %w", name, err)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", name, err)
	}

	// Read stdout line by line, forwarding to callback and collecting full output
	var stdout bytes.Buffer
	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024) // up to 1MB lines
	for scanner.Scan() {
		line := scanner.Text()
		stdout.WriteString(line)
		stdout.WriteByte('\n')
		if onLine != nil {
			onLine(line)
		}
	}

	err = cmd.Wait()
	duration := time.Since(start)

	result := &CmdResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: duration,
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
			return result, fmt.Errorf("exec %s: %w", name, err)
		}
	}

	return result, nil
}

func runCmd(ctx context.Context, name string, args []string, dir string, env []string, stdin io.Reader) (*CmdResult, error) {
	if strings.HasSuffix(name, ".dll") {
		args = append([]string{name}, args...)
		// Resolve dotnet binary: portable → x64 system → PATH fallback.
		// PATH often resolves to x86 dotnet which may lack required runtimes,
		// while x64 (Program Files) typically has the latest release.
		localDotnet := filepath.Join(BaseDir(), "runtimes", "dotnet", "dotnet.exe")
		x64Dotnet := filepath.Join(os.Getenv("ProgramW6432"), "dotnet", "dotnet.exe")
		if _, err := os.Stat(localDotnet); err == nil {
			name = localDotnet
		} else if _, err := os.Stat(x64Dotnet); err == nil {
			name = x64Dotnet
		} else {
			name = "dotnet"
		}
		// Allow .NET to roll forward to nearest compatible runtime version.
		// Prevents failures when tool targets e.g. net10.0 but only 10.0.x-rc is installed.
		env = append(env, "DOTNET_ROLL_FORWARD=LatestMajor")
	}

	start := time.Now()

	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}

	if stdin != nil {
		cmd.Stdin = stdin
	}

	HideCmdWindow(cmd)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	duration := time.Since(start)

	result := &CmdResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: duration,
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
			return result, fmt.Errorf("exec %s: %w", name, err)
		}
	}

	return result, nil
}
