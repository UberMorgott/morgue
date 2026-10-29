package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/UberMorgott/morgue/internal/recipe"
	"github.com/UberMorgott/morgue/internal/runmodel"
)

// consume projects structured pipeline data into the single serialized stream.
// Human messages remain logs and never supply counters or lifecycle state.
func (r *Run) consume(opts Options, ev PipelineEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := r.keys[targetKey(opts, ev.Target)]
	if target := r.targets[id]; target != nil && !target.announced {
		id = ""
	}
	parent := opts.parentTargetID
	if ev.Done {
		err := errors.Join(ev.Error, r.eventErr)
		outcome := ev.RunOutcome
		if err != nil {
			outcome = errorOutcome(err)
		}
		for _, targetID := range r.order {
			t := r.targets[targetID]
			if !t.finished {
				terminal := outcome
				if terminal == OutcomeSuccess {
					terminal = OutcomeError
				}
				r.finishTargetLocked(t, terminal, errorText(err), "")
			}
		}
		if summaryErr := r.writeSummaryLocked(); summaryErr != nil {
			r.eventErr = errors.Join(r.eventErr, fmt.Errorf("write summary: %w", summaryErr))
		}
		err = errors.Join(ev.Error, r.eventErr)
		if err != nil {
			outcome = errorOutcome(err)
		}
		if r.persistenceErr != nil {
			outcome = OutcomeError
		}
		targets := make([]runmodel.TargetResult, 0, len(r.order))
		for _, targetID := range r.order {
			targets = append(targets, r.targets[targetID].model)
		}
		counts, leaves := countTargets(targets, false), countTargets(targets, true)
		r.result = runmodel.RunResult{Handle: r.handle, Outcome: outcome, Targets: targets, Counts: counts, LeafTargets: leaves, Error: errorText(err)}
		now := time.Now().UTC()
		r.emitLocked("", "", now, runmodel.RunFinished{Outcome: outcome, FinishedAt: now, DurationMs: time.Since(r.handle.StartedAt).Milliseconds(), Error: errorText(err), Targets: counts, LeafTargets: leaves})
		if r.persistenceErr != nil {
			r.result.Outcome = OutcomeError
			r.result.Error = r.persistenceErr.Error()
		}
		return
	}
	if ev.TargetOutcome != "" {
		return
	} // record owns target terminals, including children.
	if ev.ScanDone {
		r.emitLocked("", parent, time.Now().UTC(), runmodel.ScanCompleted{FilesTotal: int64(ev.FilesTotal), GroupsTotal: int64(ev.GroupsTotal)})
		return
	}
	if ev.Progress != nil && id != "" {
		p := ev.Progress
		stepID := p.StepID
		if stepID == "" {
			stepID = fmt.Sprintf("step-%d", p.Step)
		}
		r.emitLocked(id, parent, time.Now().UTC(), runmodel.StepProgress{StepID: stepID, Name: p.Name, Tool: p.Tool, Unit: p.Unit, StepIndex: int64(p.Step), StepTotal: int64(p.Total), Count: int64(p.Count), CountTotal: int64(p.CountTotal), DurationMs: p.Duration.Milliseconds(), Status: strings.ToLower(p.Status.String()), Error: errorText(p.Error)})
		return
	}
	if ev.ToolStage != "" {
		message := ev.ErrorMsg
		if ev.Error != nil {
			message = ev.Error.Error()
		}
		r.emitLocked(id, parent, time.Now().UTC(), runmodel.ToolProgress{Tool: ev.Tool, Stage: ev.ToolStage, Bytes: ev.Bytes, BytesTotal: ev.BytesTotal, Version: ev.ToolVersion, Error: message})
		return
	}
	if ev.Phase == "tools" && len(ev.ToolsNeeded) > 0 {
		for _, tool := range ev.ToolsNeeded {
			r.emitLocked("", parent, time.Now().UTC(), runmodel.ToolProgress{Tool: tool, Stage: "check"})
		}
		return
	}
	level := "info"
	message := ev.Message
	if ev.Severity == "warn" {
		level = "warn"
	}
	if ev.Error != nil {
		level = "error"
		message = ev.Error.Error()
	}
	r.emitLocked(id, parent, time.Now().UTC(), runmodel.LogRecord{Level: level, Tool: ev.Tool, Message: message, Phase: ev.Phase})
}

// LegacyEvent adapts v2 to old renderers. Target carries the stable target ID;
// discovery data retains the original input path for clients needing a label.
func LegacyEvent(ev runmodel.Event) PipelineEvent {
	result := PipelineEvent{Target: ev.TargetID}
	switch ev.Type {
	case runmodel.EventRunStarted:
		var p runmodel.RunStarted
		if json.Unmarshal(ev.Data, &p) == nil {
			result.Phase = "scan"
			result.Target = p.Spec.Path
			result.OutputPath = p.Handle.Output
			result.Message = "Starting pipeline"
		}
	case runmodel.EventScanCompleted:
		var p runmodel.ScanCompleted
		if json.Unmarshal(ev.Data, &p) == nil {
			result.Phase = "scan"
			result.FilesTotal = int(p.FilesTotal)
			result.GroupsTotal = int(p.GroupsTotal)
			result.Message = fmt.Sprintf("Found %d files in %d groups", p.FilesTotal, p.GroupsTotal)
		}
	case runmodel.EventTargetDiscovered:
		var p runmodel.TargetDiscovered
		if json.Unmarshal(ev.Data, &p) == nil {
			result.Phase = "match"
			result.ReconKind = p.Kind
			result.RecipeName = p.Recipe
			result.RecipeDesc = p.RecipeDesc
			result.Compiler = p.Compiler
			result.Obfuscator = p.Obfuscator
			result.Deobfuscator = p.Deobfuscator
			result.FileSize = p.SizeBytes
			result.ToolsNeeded = p.ToolsNeeded
			result.OutputPath = p.Output
			result.Message = p.InputPath
		}
	case runmodel.EventTargetFinished:
		var p runmodel.TargetFinished
		if json.Unmarshal(ev.Data, &p) == nil {
			result.Phase = "target"
			result.TargetOutcome = p.Outcome
			result.OutputPath = p.Output
			result.RecipeName = p.Recipe
			result.ErrorMsg = p.Error
			result.Message = p.SkipReason
			if p.Error != "" {
				result.Error = errors.New(p.Error)
			}
		}
	case runmodel.EventStepProgress:
		var p runmodel.StepProgress
		if json.Unmarshal(ev.Data, &p) == nil {
			status := recipe.Pending
			switch p.Status {
			case "running":
				status = recipe.Running
			case "success":
				status = recipe.Success
			case "failed":
				status = recipe.Failed
			case "skipped":
				status = recipe.Skipped
			case "warn":
				status = recipe.Warn
			}
			progress := &recipe.StepProgress{StepID: p.StepID, Step: int(p.StepIndex), Total: int(p.StepTotal), Name: p.Name, Tool: p.Tool, Count: int(p.Count), CountTotal: int(p.CountTotal), Unit: p.Unit, Status: status, Duration: time.Duration(p.DurationMs) * time.Millisecond}
			if p.Error != "" {
				progress.Error = errors.New(p.Error)
			}
			result.Phase = "execute"
			result.Tool = p.Tool
			result.Progress = progress
			if status == recipe.Warn {
				result.Severity = "warn"
			}
		}
	case runmodel.EventToolProgress:
		var p runmodel.ToolProgress
		if json.Unmarshal(ev.Data, &p) == nil {
			result.Phase = "tools"
			result.Tool = p.Tool
			result.ToolStage = p.Stage
			result.Bytes = p.Bytes
			result.BytesTotal = p.BytesTotal
			result.Message = p.Stage
			result.ToolsNeeded = []string{p.Tool}
			if p.Stage == "download" {
				result.Phase = "download"
			}
			if p.Stage == "extract" {
				result.Phase = "extract"
			}
			if p.Error != "" {
				result.Error = errors.New(p.Error)
				result.ErrorMsg = p.Error
			}
		}
	case runmodel.EventLog:
		var p runmodel.LogRecord
		if json.Unmarshal(ev.Data, &p) == nil {
			result.Phase = p.Phase
			if result.Phase == "" {
				result.Phase = "log"
			}
			result.Tool = p.Tool
			result.Message = p.Message
			if p.Level == "warn" {
				result.Severity = "warn"
			}
			if p.Level == "error" {
				result.Error = errors.New(p.Message)
				result.ErrorMsg = p.Message
			}
		}
	case runmodel.EventRunFinished:
		var p runmodel.RunFinished
		if json.Unmarshal(ev.Data, &p) == nil {
			result.Phase = "done"
			result.Done = true
			result.RunOutcome = p.Outcome
			result.FilesTotal = int(p.Targets.Total)
			result.FilesProcessed = int(p.Targets.Total - p.Targets.Pending - p.Targets.Running)
			if p.Outcome != OutcomeSuccess {
				result.Phase = p.Outcome
			}
			result.Message = "Pipeline complete"
			if p.Error != "" {
				result.Error = errors.New(p.Error)
				result.ErrorMsg = p.Error
				result.Message = p.Error
			}
		}
	}
	return result
}
