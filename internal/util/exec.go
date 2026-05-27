package util

import (
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

func runCmd(ctx context.Context, name string, args []string, dir string, env []string, stdin io.Reader) (*CmdResult, error) {
	if strings.HasSuffix(name, ".dll") {
		args = append([]string{name}, args...)
		// Prefer local portable dotnet from tools base dir before falling back to PATH
		localDotnet := filepath.Join(BaseDir(), "runtimes", "dotnet", "dotnet.exe")
		if _, err := os.Stat(localDotnet); err == nil {
			name = localDotnet
		} else {
			name = "dotnet"
		}
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
