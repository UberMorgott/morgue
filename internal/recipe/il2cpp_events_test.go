package recipe

import (
	"testing"
)

// TestExecuteStepIndicesContiguous verifies the recipe's Steps() surface and that
// every step index in Steps() is a valid report target (0..len-1). This locks the
// step contract the webview/stepper relies on: each new stage has a stable index the
// engine forwards as PipelineEvent.Progress.Step.
func TestExecuteStepIndicesContiguous(t *testing.T) {
	i := &IL2CPP{}
	steps := i.Steps()
	if len(steps) != 7 {
		t.Fatalf("expected 7 steps (copy, extract, decompile, data, odin, strings, index), got %d: %+v", len(steps), steps)
	}
	// The data + odin stages must sit at indices 3 and 4 (between decompile and strings).
	if steps[3].Name != "Extract data layer" {
		t.Fatalf("step 3 = %q, want 'Extract data layer'", steps[3].Name)
	}
	if steps[4].Name != "Decode Odin config" {
		t.Fatalf("step 4 = %q, want 'Decode Odin config'", steps[4].Name)
	}
}

// TestStepProgressShape confirms a StepProgress sent on a buffered channel is shaped
// the way the engine forwarder expects (Step/Total/Name/Tool/Status), i.e. the fields
// the webview maps in pipeline.ts updateFromEvent.
func TestStepProgressShape(t *testing.T) {
	ch := make(chan StepProgress, 1)
	ch <- StepProgress{Step: 3, Total: 7, Name: "Extract data layer", Tool: "assetripper", Status: Running, Count: 12, Unit: "assets"}
	p := <-ch
	if p.Step != 3 || p.Total != 7 || p.Tool != "assetripper" || p.Unit != "assets" {
		t.Fatalf("StepProgress shape wrong: %+v", p)
	}
}
