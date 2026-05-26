package util

import (
	"context"
	"testing"
	"time"
)

func TestRunCmd(t *testing.T) {
	ctx := context.Background()
	result, err := RunCmd(ctx, "go", []string{"version"}, "")
	if err != nil {
		t.Fatalf("RunCmd() error: %v", err)
	}

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

	result, _ := RunCmd(ctx, "go", []string{"version"}, "")
	// go version is fast enough it might finish; just ensure no panic
	_ = result
}

func TestRunCmdBadCommand(t *testing.T) {
	ctx := context.Background()
	result, err := RunCmd(ctx, "nonexistent_binary_xyz", nil, "")

	if err == nil {
		t.Error("RunCmd() should return error for missing binary")
	}
	if result.ExitCode == 0 {
		t.Error("ExitCode should be non-zero for missing binary")
	}
}
