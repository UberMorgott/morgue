package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/UberMorgott/morgue/internal/config"
)

// runAndDrain runs the pipeline while draining its event channel.
func runAndDrain(t *testing.T, e *Engine, opts Options) {
	t.Helper()
	ch := make(chan PipelineEvent, 64)
	done := make(chan struct{})
	go func() {
		for range ch {
		}
		close(done)
	}()
	err := e.Run(context.Background(), opts, ch)
	close(ch)
	<-done
	if err != nil && !errors.Is(err, ErrNoSuccessfulTargets) {
		t.Fatalf("Run() error: %v", err)
	}
}

// TestSummarySkippedEntriesHavePath verifies skip-list and exclude results
// carry the source path into summary.json instead of an empty string.
func TestSummarySkippedEntriesHavePath(t *testing.T) {
	in := t.TempDir()
	out := t.TempDir()
	skipped := filepath.Join(in, "mscorlib.dll")
	excluded := filepath.Join(in, "Excluded.dll")
	for _, p := range []string{skipped, excluded} {
		if err := os.WriteFile(p, []byte("fake"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	e := New(config.Default(), t.TempDir())
	runAndDrain(t, e, Options{Input: in, Output: out, Exclude: []string{"Excluded.dll"}})

	data, err := os.ReadFile(filepath.Join(out, "summary.json"))
	if err != nil {
		t.Fatalf("read summary.json: %v", err)
	}
	var s struct {
		Results []summaryEntry `json:"results"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("decode summary.json: %v", err)
	}
	got := map[string]string{}
	for _, r := range s.Results {
		if r.Path == "" {
			t.Errorf("summary entry with empty path: %+v", r)
		}
		got[r.Path] = r.SkipReason
	}
	if _, ok := got[skipped]; !ok {
		t.Errorf("skip-listed file %q missing from summary: %v", skipped, got)
	}
	if got[excluded] != "excluded" {
		t.Errorf("excluded file %q: reason %q, want \"excluded\"", excluded, got[excluded])
	}
}
