package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

func (m *Manager) installRuntimeLocked(ctx context.Context, kind RuntimeKind, cb *InstallCallbacks) error {
	if runtimeBinary(kind) == "" {
		return fmt.Errorf("unknown runtime: %s", kind)
	}
	stageRoot, err := os.MkdirTemp(m.baseDir, ".runtime-staging-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stageRoot)
	staged := &Manager{baseDir: stageRoot, cfg: m.cfg, state: m.state, runtimeToolBase: m.baseDir}
	if err = staged.installRuntimeInto(ctx, kind, cb); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if _, err = os.Stat(staged.localRuntimeBin(kind)); err != nil {
		return err
	}
	if _, err = inventoryHash(ctx, staged.localRuntimeDir(kind)); err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Join(m.baseDir, "runtimes"), 0755); err != nil {
		return err
	}
	return swapDir(staged.localRuntimeDir(kind), m.localRuntimeDir(kind))
}
