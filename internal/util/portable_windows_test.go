//go:build windows

package util

import (
	"os"
	"testing"
)

func TestSysinternalsEulaKey(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{`C:\m\tools\strings\strings64.exe`, []string{"-nobanner", "-accepteula", "x"}, "Strings"},
		{`C:\m\tools\strings\STRINGS.EXE`, []string{"-AcceptEula"}, "Strings"},
		{`C:\m\tools\strings\strings64.exe`, []string{"-nobanner", "x"}, ""}, // no EULA write
		{`C:\m\tools\ilspycmd\ilspycmd.exe`, []string{"-accepteula"}, ""},
	}
	for _, c := range cases {
		if got := sysinternalsEulaKey(c.name, c.args); got != c.want {
			t.Errorf("sysinternalsEulaKey(%q, %v) = %q, want %q", c.name, c.args, got, c.want)
		}
	}
}

func TestProcessAlive(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Error("own process reported dead")
	}
	if processAlive(999999999) {
		t.Error("impossible pid reported alive")
	}
}
