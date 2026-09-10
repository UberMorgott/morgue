package cli

import (
	"io"
	"os"
	"strings"
	"testing"
)

// captureStderr swaps os.Stderr for a pipe, runs fn, and returns everything
// written to stderr while fn ran.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	fn()
	_ = w.Close()
	return <-done
}

// TestRunWatchNonTTYNote verifies Issue 3: when --watch is requested but stderr
// is not a terminal, Run degrades to the plain progress log and prints the
// explicit note instead of silently disabling the TUI.
func TestRunWatchNonTTYNote(t *testing.T) {
	// Force the non-interactive branch regardless of the real test stderr.
	orig := stderrIsTerminal
	stderrIsTerminal = func() bool { return false }
	defer func() { stderrIsTerminal = orig }()

	out := captureStderr(t, func() {
		// Target does not exist, so the pipeline scan fails fast right after the
		// branch decision — we only care that the note was emitted and RunWatch
		// (the TUI) was NOT taken.
		_ = Run(RunOptions{
			Target: filepathNonexistent(t),
			Output: t.TempDir(),
			Watch:  true,
			Quiet:  false,
		})
	})

	if !strings.Contains(out, "stderr is not a TTY") {
		t.Errorf("expected non-TTY note on stderr, got:\n%s", out)
	}
	if !strings.Contains(out, "use -q for JSON-only output") {
		t.Errorf("expected -q hint in note, got:\n%s", out)
	}
}

func filepathNonexistent(t *testing.T) string {
	t.Helper()
	return t.TempDir() + string(os.PathSeparator) + "does-not-exist-xyz.exe"
}
