package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/recon"
)

func drain(ch chan PipelineEvent) []PipelineEvent {
	var evs []PipelineEvent
	for ev := range ch {
		evs = append(evs, ev)
	}
	return evs
}

func hasPhaseContaining(evs []PipelineEvent, phase, substr string) bool {
	for _, ev := range evs {
		if ev.Phase == phase && strings.Contains(ev.Message, substr) {
			return true
		}
	}
	return false
}

// makeExtractedTarget builds a target-output dir containing a non-empty
// extracted/ tree and returns a matching NSIS TargetResult.
func makeExtractedTarget(t *testing.T) TargetResult {
	t.Helper()
	targetOut := filepath.Join(t.TempDir(), "installer.exe")
	extracted := filepath.Join(targetOut, "extracted")
	if err := os.MkdirAll(extracted, 0755); err != nil {
		t.Fatal(err)
	}
	// A plain text file: the nested run classifies it as non-binary and skips it,
	// keeping the recursion fast and side-effect-free.
	if err := os.WriteFile(filepath.Join(extracted, "readme.txt"), []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}
	return TargetResult{Recon: recon.Result{Kind: recon.NSIS}, Output: targetOut}
}

// TestMaybeRecurseUnpack_DepthGuard verifies the recursion stops at the cap and
// emits a WARN instead.
func TestMaybeRecurseUnpack_DepthGuard(t *testing.T) {
	e := New(config.Default(), t.TempDir())
	tr := makeExtractedTarget(t)

	ch := make(chan PipelineEvent, 100)
	e.maybeRecurseUnpack(context.Background(), &Options{Depth: maxUnpackDepth}, tr, emitter{ch: ch})
	close(ch)
	evs := drain(ch)

	if !hasPhaseContaining(evs, "unpack", "max unpack depth") {
		t.Error("expected a WARN about max unpack depth at the cap")
	}
	if hasPhaseContaining(evs, "unpack", "Unpacked installer") {
		t.Error("must NOT recurse when at the depth cap")
	}
}

// TestMaybeRecurseUnpack_RecursesBelowCap verifies a below-cap NSIS result with a
// non-empty extracted tree triggers a nested run.
func TestMaybeRecurseUnpack_RecursesBelowCap(t *testing.T) {
	e := New(config.Default(), t.TempDir())
	tr := makeExtractedTarget(t)

	ch := make(chan PipelineEvent, 200)
	e.maybeRecurseUnpack(context.Background(), &Options{Depth: 0}, tr, emitter{ch: ch})
	close(ch)
	evs := drain(ch)

	if !hasPhaseContaining(evs, "unpack", "Unpacked installer") {
		t.Error("expected a recursing event below the depth cap")
	}
	// The nested run (Depth 1) must NOT emit a terminal "done".
	for _, ev := range evs {
		if ev.Done {
			t.Error("nested unpack run must not emit a terminal done event")
		}
	}
	// The recursion wrote its results under <target>/decompiled.
	if _, err := os.Stat(filepath.Join(tr.Output, "decompiled")); err != nil {
		t.Errorf("expected decompiled/ output dir from nested run: %v", err)
	}
}

// TestMaybeRecurseUnpack_SkipsNonNSIS verifies non-installer results never
// recurse.
func TestMaybeRecurseUnpack_SkipsNonNSIS(t *testing.T) {
	e := New(config.Default(), t.TempDir())
	tr := makeExtractedTarget(t)
	tr.Recon.Kind = recon.Native // not an installer

	ch := make(chan PipelineEvent, 100)
	e.maybeRecurseUnpack(context.Background(), &Options{Depth: 0}, tr, emitter{ch: ch})
	close(ch)
	evs := drain(ch)

	if len(evs) != 0 {
		t.Errorf("non-NSIS result must not emit unpack events, got %d", len(evs))
	}
}
