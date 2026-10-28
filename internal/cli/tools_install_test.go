package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/UberMorgott/morgue/internal/tools"
)

type fakeInstaller struct {
	fail      map[string]bool
	installed []string
}

func (f *fakeInstaller) IsInstalled(string) bool { return false }

func (f *fakeInstaller) Install(_ context.Context, name string, _ *tools.InstallCallbacks) (string, error) {
	if f.fail[name] {
		return "", errors.New("boom")
	}
	f.installed = append(f.installed, name)
	return "1.0", nil
}

// TestInstallManyFailureIsError: a bulk install with failures must keep going
// and return an error naming the failed tools (non-zero exit), not exit 0.
func TestInstallManyFailureIsError(t *testing.T) {
	first, second := tools.Registry[0].Name, tools.Registry[1].Name
	f := &fakeInstaller{fail: map[string]bool{first: true, second: true}}

	err := installMany(t.Context(), f, false, nil)
	if err == nil {
		t.Fatal("installMany returned nil despite failures")
	}
	if !strings.Contains(err.Error(), first) || !strings.Contains(err.Error(), second) {
		t.Fatalf("error does not name failed tools: %v", err)
	}
	if want := len(tools.Registry) - 2; len(f.installed) != want {
		t.Fatalf("installed %d tools after failures, want %d", len(f.installed), want)
	}

	if err := installMany(t.Context(), &fakeInstaller{}, false, nil); err != nil {
		t.Fatalf("all-success bulk install returned %v", err)
	}
}
