package engine

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestPipelineEventWireError(t *testing.T) {
	err := errors.New("decompiler exited with code 1")
	for _, tc := range []struct {
		name  string
		event PipelineEvent
	}{
		{"literal", PipelineEvent{Error: err}},
		{"emitter", func() PipelineEvent {
			ch := make(chan PipelineEvent, 1)
			emitter{ch: ch}.emitErr("execute", "App.dll", err)
			return <-ch
		}()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, marshalErr := json.Marshal(tc.event)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			var wire map[string]any
			if err := json.Unmarshal(data, &wire); err != nil {
				t.Fatal(err)
			}
			if wire["error"] != err.Error() {
				t.Fatalf("wire error = %#v, want %q", wire["error"], err.Error())
			}
			if _, found := wire["Error"]; found {
				t.Fatal("Go error leaked into JSON DTO")
			}
			if !errors.Is(tc.event.Error, err) {
				t.Fatal("internal error was lost")
			}
		})
	}
}

func TestPipelineEventWarningHasNoWireError(t *testing.T) {
	ch := make(chan PipelineEvent, 1)
	emitter{ch: ch}.emitWarn("tools", "App.dll", "optional tool missing")
	data, err := json.Marshal(<-ch)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if _, found := wire["error"]; found {
		t.Fatal("warning has error payload")
	}
}

func TestPipelineEventOutcomeFields(t *testing.T) {
	for _, outcome := range []string{OutcomeSuccess, OutcomeError, OutcomeCancelled, OutcomeSkipped} {
		t.Run(outcome, func(t *testing.T) {
			data, err := json.Marshal(PipelineEvent{RunOutcome: outcome, TargetOutcome: outcome})
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]any
			if err := json.Unmarshal(data, &wire); err != nil {
				t.Fatal(err)
			}
			if wire["RunOutcome"] != outcome || wire["TargetOutcome"] != outcome {
				t.Fatalf("outcomes lost in %s", data)
			}
		})
	}
}
