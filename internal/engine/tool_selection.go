package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/UberMorgott/morgue/internal/runmodel"
	"github.com/UberMorgott/morgue/internal/tools"
)

// ensureSelectedTools acquires a recipe's tools through the run selection. The
// selection performs the single forced freshness check per distinct tool and
// run, and remembers failures, so repeated targets and nested children never
// re-probe or re-install a name. Required/optional policy stays here: a missing
// optional tool is an explicit warning, a missing required tool fails the target.
// There is no retry loop: one decision per tool and run.
func (e *Engine) ensureSelectedTools(ctx context.Context, filePath string, names []string, em emitter) error {
	sel := em.run.selection
	lastPct := -1
	lastEmit := time.Now()
	cb := &tools.InstallCallbacks{
		OnProgress: func(tool string, bytesDown, bytesTotal int64) {
			pct := 0
			if bytesTotal > 0 {
				pct = int(bytesDown * 100 / bytesTotal)
			}
			if pct != lastPct || pct >= 100 || time.Since(lastEmit) >= time.Second {
				lastPct = pct
				lastEmit = time.Now()
				em.send(PipelineEvent{
					Phase: "download", Tool: tool, ToolStage: "download",
					Bytes: max(bytesDown, 0), BytesTotal: max(bytesTotal, 0), Target: filePath,
					Message: fmt.Sprintf("Downloading %s... %d%%", tool, pct),
				})
			}
		},
		OnExtract: func(tool string) {
			em.send(PipelineEvent{Phase: "extract", Tool: tool, ToolStage: "extract", Target: filePath, Message: fmt.Sprintf("Extracting %s...", tool)})
		},
		OnStatus: func(st tools.ToolPolicyStatus) {
			switch st.Freshness {
			case "checking":
				em.send(PipelineEvent{Phase: "tools", Tool: st.Name, ToolStage: "check", Target: filePath, Message: fmt.Sprintf("Checking %s for updates...", st.Name)})
			case "warning":
				// Tool-scoped log record (level=warn, tool=<name>), emitted
				// inside Ensure, i.e. before this tool's "ready" event, so the
				// UI attaches the freshness warning to the right tool.
				em.send(PipelineEvent{Phase: "tools", Tool: st.Name, Target: filePath, Severity: "warn", Message: fmt.Sprintf("%s: %s", st.Name, st.Warning)})
			}
		},
	}
	var fatal []string
	for _, name := range names {
		ids, err := sel.Ensure(ctx, []string{name}, cb)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			em.send(PipelineEvent{Phase: "tools", Target: filePath, Tool: name, ToolStage: "failed", ErrorMsg: err.Error()})
			def, known := tools.FindByName(name)
			optional := known && def.Optional
			sev, msg := installFailureSeverity(name, err, optional, 1)
			if sev == "warn" {
				em.emitWarn("tools", filePath, msg)
			} else {
				em.emitErr("tools", filePath, errors.New(msg))
			}
			if !optional {
				fatal = append(fatal, name)
			}
			continue
		}
		version := ""
		if len(ids) > 0 {
			version = ids[0].Version
		}
		em.send(PipelineEvent{Phase: "tools", Target: filePath, Tool: name, ToolStage: "ready", ToolVersion: version})
	}
	if len(fatal) > 0 {
		err := fmt.Errorf("required tools unavailable: %v", fatal)
		em.emitErr("tools", filePath, err)
		return err
	}
	return persistErr(em.run.persistToolsLock(false))
}

func persistErr(err error) error {
	if err != nil {
		return fmt.Errorf("record run tool lock: %w", err)
	}
	return nil
}

// persistToolsLock atomically replaces the workspace's .morgue/tools.lock.json
// with the selection snapshot. complete=true seals acquisition and makes the
// file immutable for the rest of the run.
func (r *Run) persistToolsLock(complete bool) error {
	if r == nil || r.selection == nil || r.spec.Output == "" {
		return nil
	}
	r.toolsLockMu.Lock()
	defer r.toolsLockMu.Unlock()
	if r.toolsLockSealed {
		if complete {
			return nil
		}
		return errors.New("run tool lock is already final")
	}
	lock := r.selection.Snapshot(complete)
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(r.spec.Output, filepath.FromSlash(runmodel.RunToolsLockPath)), append(data, '\n')); err != nil {
		return err
	}
	if complete {
		r.toolsLockSealed = true
	}
	return nil
}

// toolVersion returns the frozen version of a selected tool, or "" when the
// tool was not selected in this run.
func (r *Run) toolVersion(name string) string {
	if r == nil || r.selection == nil || name == "" {
		return ""
	}
	for _, id := range r.selection.Snapshot(false).Tools {
		if id.Name == name {
			return id.Version
		}
	}
	return ""
}

func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".tools-lock-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
