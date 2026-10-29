package tools

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/util"
)

// Invoked only as an owned subprocess by the parent tests below.
func TestToolsLeaseChild(t *testing.T) {
	base := os.Getenv("MORGUE_TEST_TOOL_LEASE_BASE")
	if base == "" {
		return
	}
	if name := os.Getenv("MORGUE_TEST_TOOL_RECORD_NAME"); name != "" {
		m := NewManager(base, config.Config{})
		if err := m.RecordInstall(name, "1"); err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, name := range []string{"process-tool", "runtime:java"} {
		lease, err := acquireToolLock(t.Context(), base, name, false, true)
		if err != nil {
			t.Fatal(err)
		}
		defer lease.Close()
	}
	if err := os.WriteFile(filepath.Join(base, "ready"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(base, "release")); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("parent did not release child")
}

func toolsChildCommand(t *testing.T, base, name string) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestToolsLeaseChild$")
	util.ApplyChildEnv(cmd)
	util.HideCmdWindow(cmd)
	cmd.Env = append(cmd.Env, "MORGUE_TEST_TOOL_LEASE_BASE="+base, "MORGUE_TEST_TOOL_RECORD_NAME="+name)
	return cmd
}

func TestToolAndRuntimeLeasesAcrossProcesses(t *testing.T) {
	base := t.TempDir()
	cmd := toolsChildCommand(t, base, "")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.WriteFile(filepath.Join(base, "release"), nil, 0600); _ = cmd.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(base, "ready")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child lease readiness timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}
	onlyTestTool(t, ToolDef{Name: "process-tool", Binary: "tool.exe"})
	m := NewManager(base, config.Config{})
	if err := m.Delete("process-tool"); !errors.Is(err, ErrInUse) {
		t.Fatalf("cross-process delete: %v", err)
	}
	if _, err := m.Install(t.Context(), "process-tool", nil); !errors.Is(err, ErrInUse) {
		t.Fatalf("cross-process install: %v", err)
	}
	if err := m.InstallRuntime(RuntimeJava, nil); !errors.Is(err, ErrInUse) {
		t.Fatalf("cross-process runtime install: %v", err)
	}
	shared, err := acquireToolLock(t.Context(), base, "process-tool", false, false)
	if err != nil {
		t.Fatalf("cross-process shared lease: %v", err)
	}
	_ = shared.Close()
	if err := os.WriteFile(filepath.Join(base, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	lease, err := acquireToolLock(t.Context(), base, "process-tool", true, false)
	if err != nil {
		t.Fatalf("released child left stale lock: %v", err)
	}
	_ = lease.Close()
}

func TestInstalledLockCrossProcessWritersPreserveEntries(t *testing.T) {
	base := t.TempDir()
	a := toolsChildCommand(t, base, "writer-a")
	b := toolsChildCommand(t, base, "writer-b")
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	if err := b.Start(); err != nil {
		t.Fatal(err)
	}
	if err := a.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := b.Wait(); err != nil {
		t.Fatal(err)
	}
	lk, err := ReadInstalledLock(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(lk.Tools) != 2 || lk.Tools["writer-a"].Version != "1" || lk.Tools["writer-b"].Version != "1" {
		t.Fatalf("lost entries: %+v", lk)
	}
}
