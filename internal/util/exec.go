package util

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
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

// RunCmdWithEnv executes a command with custom environment variables.
// env is a list of "KEY=VALUE" strings that are appended to the current environment.
// If env is nil, the default environment is used.
func RunCmdWithEnv(ctx context.Context, name string, args []string, dir string, env []string) (*CmdResult, error) {
	if strings.HasSuffix(name, ".dll") {
		args = append([]string{name}, args...)
		name = "dotnet"
	}

	start := time.Now()

	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
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
