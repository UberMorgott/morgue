package cli

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/UberMorgott/morgue/internal/engine"
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

func TestHasRunSummary(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"success", nil, true},
		{"target failures", engine.ErrTargetsFailed, true},
		{"no successful targets", engine.ErrNoSuccessfulTargets, true},
		{"scan or write error", errors.New("scan failed"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasRunSummary(tc.err); got != tc.want {
				t.Fatalf("hasRunSummary() = %v, want %v", got, tc.want)
			}
		})
	}
}

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	original := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = original }()
	done := make(chan string, 1)
	go func() { data, _ := io.ReadAll(r); _ = r.Close(); done <- string(data) }()
	runErr := fn()
	_ = w.Close()
	return <-done, runErr
}

func TestRunQuietUnsuccessfulRunKeepsJSONSummary(t *testing.T) {
	out, err := captureStdout(t, func() error {
		return Run(RunOptions{Target: t.TempDir(), Output: t.TempDir(), Quiet: true})
	})
	if !errors.Is(err, engine.ErrNoSuccessfulTargets) {
		t.Fatalf("Run() = %v, want no successful targets", err)
	}
	var summary engine.PipelineSummary
	if err := json.Unmarshal([]byte(out), &summary); err != nil {
		t.Fatalf("stdout is not summary JSON: %q: %v", out, err)
	}
	if summary.Stats.Total != 0 {
		t.Fatalf("total = %d, want 0", summary.Stats.Total)
	}
}

func TestRunScanFailureDoesNotPrintStaleSummary(t *testing.T) {
	output := t.TempDir()
	if err := os.WriteFile(filepath.Join(output, "summary.json"), []byte(`{"stats":{"total":99,"success":99}}`), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error {
		return Run(RunOptions{Target: filepath.Join(t.TempDir(), "missing"), Output: output, Quiet: true})
	})
	if err == nil {
		t.Fatal("missing input succeeded")
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("stale summary printed after scan failure: %s", out)
	}
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
