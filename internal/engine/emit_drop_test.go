package engine

import (
	"context"
	"testing"
	"time"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/runmodel"
)

// TestExecuteDeliversTerminalEventsUnderBackPressure proves the bounded-delivery
// policy end to end: with a one-slot buffer and a slow (but draining) consumer
// the engine never stalls, yet every lifecycle/outcome event is delivered — the
// run never loses run.started, a target's terminal, or run.finished even while
// the buffer is repeatedly full. Progress/log notifications may be dropped.
func TestExecuteDeliversTerminalEventsUnderBackPressure(t *testing.T) {
	e := New(config.Default(), t.TempDir())
	r, err := e.PrepareRun(context.Background(), runmodel.RunRequest{
		Path:   terminalInput(t, "a.dll", "b.dll", "c.dll"),
		Output: t.TempDir(),
		Recipe: terminalRecipeName,
	})
	if err != nil {
		t.Fatal(err)
	}

	// cap 1 keeps the buffer full for most of the run; the 1ms sleep makes the
	// consumer slower than the producer.
	ch := make(chan runmodel.Event, 1)
	var got []runmodel.Event
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range ch {
			got = append(got, ev)
			time.Sleep(time.Millisecond)
		}
	}()

	result, err := r.Execute(context.Background(), nil, ch)
	close(ch)
	<-done
	if err != nil {
		t.Fatal(err)
	}

	if len(got) < 2 || got[0].Type != runmodel.EventRunStarted || got[len(got)-1].Type != runmodel.EventRunFinished {
		t.Fatalf("lifecycle framing lost under back-pressure: %d events, first=%v last=%v",
			len(got), firstType(got), lastType(got))
	}
	finished := 0
	seq := uint64(0)
	for _, ev := range got {
		if ev.Seq <= seq {
			t.Fatalf("out-of-order delivery: seq %d after %d", ev.Seq, seq)
		}
		seq = ev.Seq
		if ev.Type == runmodel.EventTargetFinished {
			finished++
		}
	}
	if finished != len(result.Targets) || len(result.Targets) != 3 {
		t.Fatalf("dropped target terminals: delivered %d of %d", finished, len(result.Targets))
	}
}

func firstType(e []runmodel.Event) runmodel.EventType {
	if len(e) == 0 {
		return ""
	}
	return e[0].Type
}
func lastType(e []runmodel.Event) runmodel.EventType {
	if len(e) == 0 {
		return ""
	}
	return e[len(e)-1].Type
}
