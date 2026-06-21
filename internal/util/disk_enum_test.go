package util

import "testing"

func TestEnumerateDrivesReturnsSomething(t *testing.T) {
	drives := EnumerateDrives()
	// On the dev/CI Windows box there is always at least C:.
	if len(drives) == 0 {
		t.Skip("no fixed drives reported (non-Windows or restricted env)")
	}
	for _, d := range drives {
		if len(d.Letter) != 2 || d.Letter[1] != ':' {
			t.Fatalf("bad drive letter %q", d.Letter)
		}
	}
}
