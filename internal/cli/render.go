package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/UberMorgott/morgue/internal/runmodel"
)

// lineRenderer prints events v2 as plain progress lines (the non-TUI CLI view).
// It reads only typed payload fields, never parses human message text, and
// skips unknown event types so newer producers do not break older renderers.
type lineRenderer struct {
	w io.Writer
	// labels maps target IDs to their relative input, the human-readable name.
	labels map[string]string
	// lastSeq rejects duplicate or stale deliveries.
	lastSeq uint64
	run     string
}

func newLineRenderer(w io.Writer) *lineRenderer {
	return &lineRenderer{w: w, labels: map[string]string{}}
}

func (r *lineRenderer) label(ev runmodel.Event) string {
	if l, ok := r.labels[ev.TargetID]; ok && l != "" {
		return l
	}
	if ev.TargetID != "" {
		return ev.TargetID
	}
	return string(ev.Type)
}

// Render prints one event. It returns an error only for a malformed payload of
// a known type; callers may log it and continue.
func (r *lineRenderer) Render(ev runmodel.Event) error {
	if ev.Run != r.run {
		r.run, r.lastSeq = ev.Run, 0
		r.labels = map[string]string{}
	}
	if ev.Seq <= r.lastSeq {
		return nil
	}
	r.lastSeq = ev.Seq

	switch ev.Type {
	case runmodel.EventRunStarted:
		var p runmodel.RunStarted
		if err := json.Unmarshal(ev.Data, &p); err != nil {
			return err
		}
		_, err := fmt.Fprintf(r.w, "[run] %s started -> %s\n", p.Handle.ID, p.Handle.Output)
		return err
	case runmodel.EventScanCompleted:
		var p runmodel.ScanCompleted
		if err := json.Unmarshal(ev.Data, &p); err != nil {
			return err
		}
		_, err := fmt.Fprintf(r.w, "[scan] %d files, %d groups\n", p.FilesTotal, p.GroupsTotal)
		return err
	case runmodel.EventTargetDiscovered:
		var p runmodel.TargetDiscovered
		if err := json.Unmarshal(ev.Data, &p); err != nil {
			return err
		}
		name := p.RelativeInput
		if name == "" {
			name = p.InputPath
		}
		r.labels[ev.TargetID] = name
		line := fmt.Sprintf("[target] %s (%s)", name, p.Kind)
		if p.Recipe != "" {
			line += " recipe=" + p.Recipe
		}
		if p.Obfuscator != "" {
			line += " obfuscator=" + p.Obfuscator
		}
		_, err := fmt.Fprintln(r.w, line)
		return err
	case runmodel.EventStepProgress:
		var p runmodel.StepProgress
		if err := json.Unmarshal(ev.Data, &p); err != nil {
			return err
		}
		line := fmt.Sprintf("[%s] Step %d/%d: %s — %s", r.label(ev), p.StepIndex+1, p.StepTotal, p.Name, p.Status)
		if p.Count > 0 || p.CountTotal > 0 {
			if p.CountTotal > 0 {
				line += fmt.Sprintf(" %d/%d", p.Count, p.CountTotal)
			} else {
				line += fmt.Sprintf(" %d", p.Count)
			}
			if p.Unit != "" {
				line += " " + p.Unit
			}
		}
		if p.DurationMs > 0 {
			line += fmt.Sprintf(" (%s)", time.Duration(p.DurationMs)*time.Millisecond)
		}
		if p.Error != "" {
			line += ": " + p.Error
		}
		_, err := fmt.Fprintln(r.w, line)
		return err
	case runmodel.EventToolProgress:
		var p runmodel.ToolProgress
		if err := json.Unmarshal(ev.Data, &p); err != nil {
			return err
		}
		// Byte-level download ticks would flood a log; print stage transitions.
		if p.Stage == "download" && p.Bytes > 0 {
			return nil
		}
		line := fmt.Sprintf("[tool] %s %s", p.Tool, p.Stage)
		if p.Version != "" {
			line += " " + p.Version
		}
		if p.Error != "" {
			line += ": " + p.Error
		}
		_, err := fmt.Fprintln(r.w, line)
		return err
	case runmodel.EventLog:
		var p runmodel.LogRecord
		if err := json.Unmarshal(ev.Data, &p); err != nil {
			return err
		}
		tag := p.Phase
		if tag == "" {
			tag = p.Tool
		}
		if tag == "" {
			tag = "log"
		}
		var err error
		switch p.Level {
		case "warn":
			_, err = fmt.Fprintf(r.w, "[%s] WARN: %s\n", tag, p.Message)
		case "error":
			_, err = fmt.Fprintf(r.w, "[%s] ERROR: %s\n", tag, p.Message)
		default:
			_, err = fmt.Fprintf(r.w, "[%s] %s\n", tag, p.Message)
		}
		return err
	case runmodel.EventTargetFinished:
		var p runmodel.TargetFinished
		if err := json.Unmarshal(ev.Data, &p); err != nil {
			return err
		}
		line := fmt.Sprintf("[%s] %s", r.label(ev), p.Outcome)
		switch {
		case p.Error != "":
			line += ": " + p.Error
		case p.SkipReason != "":
			line += ": " + p.SkipReason
		}
		_, err := fmt.Fprintln(r.w, line)
		return err
	case runmodel.EventRunFinished:
		var p runmodel.RunFinished
		if err := json.Unmarshal(ev.Data, &p); err != nil {
			return err
		}
		if p.Outcome == runmodel.OutcomeSuccess {
			_, err := fmt.Fprintf(r.w, "[done] Pipeline complete (%s)\n", time.Duration(p.DurationMs)*time.Millisecond)
			return err
		}
		line := "[done] Pipeline " + p.Outcome
		if p.Error != "" {
			line += ": " + p.Error
		}
		_, err := fmt.Fprintln(r.w, line)
		return err
	}
	// artifact.written / quality.updated / unknown types: nothing to print yet.
	return nil
}
