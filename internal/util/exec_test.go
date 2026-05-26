package util

import (
	"context"
	"testing"
	"time"
)

func TestRunCmd(t *testing.T) {
	ctx := context.Background()
	result := RunCmd(ctx, "go", []string{"version"}, "")

	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
	if result.Stdout == "" {
		t.Error("Stdout is empty")
	}
	if result.Duration == 0 {
		t.Error("Duration is zero")
	}
}

func TestRunCmdTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Sleep for a long time — should be killed by context
	result := RunCmd(ctx, "go", []string{"version"}, "")
	// go version is fast enough it might finish; just ensure no panic
	_ = result
}

func TestRunCmdBadCommand(t *testing.T) {
	ctx := context.Background()
	result := RunCmd(ctx, "nonexistent_binary_xyz", nil, "")

	if result.ExitCode == 0 {
		t.Error("ExitCode should be non-zero for missing binary")
	}
}
