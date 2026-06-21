package util

import "testing"

func TestPickMostFreeExcludes(t *testing.T) {
	cands := []DriveInfo{
		{Letter: "C:", FreeBytes: 500},
		{Letter: "D:", FreeBytes: 100},
		{Letter: "E:", FreeBytes: 900},
		{Letter: "F:", FreeBytes: 300},
	}
	// C: and E: excluded -> among D:/F:, F: has more free.
	got := pickMostFree(cands, map[string]bool{"C:": true, "E:": true})
	if got != "F:" {
		t.Fatalf("pickMostFree = %q, want F:", got)
	}
}

func TestPickMostFreeAllExcludedFallsBackToMax(t *testing.T) {
	cands := []DriveInfo{
		{Letter: "C:", FreeBytes: 500},
		{Letter: "E:", FreeBytes: 900},
	}
	// Everything excluded -> fall back to absolute max free (E:).
	got := pickMostFree(cands, map[string]bool{"C:": true, "E:": true})
	if got != "E:" {
		t.Fatalf("fallback pickMostFree = %q, want E:", got)
	}
}
