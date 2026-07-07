package engine

import (
	"errors"
	"testing"
)

// TestInstallFailureSeverity verifies Issue 4's classification: benign cert
// noise and missing OPTIONAL tools are WARN; genuine failures of REQUIRED tools
// are ERROR.
func TestInstallFailureSeverity(t *testing.T) {
	benign := errors.New("failed to loadSystemRoots: exit status 0x800700b7")
	timeout := errors.New("dial tcp: timed out")
	other := errors.New("404 not found")

	tests := []struct {
		name     string
		err      error
		optional bool
		want     string
	}{
		{"benign noise, optional", benign, true, "warn"},
		{"benign noise, required", benign, false, "warn"},
		{"timeout, optional", timeout, true, "warn"},
		{"timeout, required", timeout, false, "error"},
		{"other failure, optional", other, true, "warn"},
		{"other failure, required", other, false, "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sev, msg := installFailureSeverity("ghidra", tt.err, tt.optional, 3)
			if sev != tt.want {
				t.Errorf("installFailureSeverity severity = %q, want %q", sev, tt.want)
			}
			if msg == "" {
				t.Error("installFailureSeverity returned empty message")
			}
		})
	}
}

// TestEmitWarn verifies a WARN event carries Severity "warn" and NO Error (so it
// is never rendered or counted as a failure).
func TestEmitWarn(t *testing.T) {
	ch := make(chan PipelineEvent, 1)
	em := emitter{ch: ch}
	em.emitWarn("tools", "target.exe", "optional tool ghidra unavailable")

	ev := <-ch
	if ev.Severity != "warn" {
		t.Errorf("Severity = %q, want \"warn\"", ev.Severity)
	}
	if ev.Error != nil {
		t.Errorf("WARN event must not set Error, got %v", ev.Error)
	}
	if ev.Message == "" {
		t.Error("WARN event should carry a Message")
	}
}
