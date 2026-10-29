package cli

import (
	"testing"

	"github.com/UberMorgott/morgue/internal/tools"
)

// `tools install NAME [--version X]` decisions shared with GUI/API policy.
func TestInstallNeeded(t *testing.T) {
	for _, tc := range []struct {
		name               string
		installed          bool
		operator, version  string
		force, wantInstall bool
	}{
		{"missing", false, "", "", false, true},
		{"installed, no pin", true, "", "", false, false},
		{"installed, forced", true, "", "", true, true},
		{"explicit version always installs (rollback)", true, "", "1.0", false, true},
		{"plain install clears an operator pin", true, "1.0", "", false, true},
		{"re-pin to another version", true, "1.0", "0.9", false, true},
	} {
		if got := installNeeded(tc.installed, tc.operator, tc.version, tc.force); got != tc.wantInstall {
			t.Errorf("%s: installNeeded = %v, want %v", tc.name, got, tc.wantInstall)
		}
	}
	if pinnedLabel(tools.ToolStatus{}) != "-" || pinnedLabel(tools.ToolStatus{OperatorVersion: "1.2"}) != "1.2" {
		t.Error("pinned label")
	}
}
