package util

import (
	"strings"
	"testing"
)

func TestOutputRootPrefersExplicit(t *testing.T) {
	got := OutputRoot("D:/explicit", []DriveInfo{{Letter: "D:", FreeBytes: 1}})
	if got != "D:/explicit" {
		t.Fatalf("explicit output not honored: %q", got)
	}
}

func TestOutputRootAvoidsCAndE(t *testing.T) {
	drives := []DriveInfo{
		{Letter: "C:", FreeBytes: 1000},
		{Letter: "D:", FreeBytes: 500},
		{Letter: "E:", FreeBytes: 2000},
	}
	got := OutputRoot("", drives)
	if strings.HasPrefix(got, "C:") || strings.HasPrefix(got, "E:") {
		t.Fatalf("auto output must avoid C:/E:, got %q", got)
	}
	if !strings.HasPrefix(got, "D:") {
		t.Fatalf("auto output should land on D:, got %q", got)
	}
}

func TestAutoOutputRootNeverEmpty(t *testing.T) {
	// Must always return a usable path (falls back to DefaultOutputDir on no drives).
	if AutoOutputRoot() == "" {
		t.Fatalf("AutoOutputRoot returned empty")
	}
}
