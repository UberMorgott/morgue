package recipe

import (
	"context"
	"errors"
	"testing"
)

func TestRunDumpFallsBackOnError(t *testing.T) {
	calls := []string{}
	// First tool (inspector) fails; second (dumper) succeeds.
	run := func(ctx context.Context, tool string) error {
		calls = append(calls, tool)
		if tool == "il2cppinspector" {
			return errors.New("inspector boom")
		}
		return nil
	}
	used, err := runDumpWithOrder(context.Background(), []string{"il2cppinspector", "il2cppdumper"}, run)
	if err != nil {
		t.Fatalf("runDumpWithOrder: %v", err)
	}
	if used != "il2cppdumper" {
		t.Fatalf("used = %q, want il2cppdumper", used)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 attempts, got %v", calls)
	}
}

func TestRunDumpAllFail(t *testing.T) {
	run := func(ctx context.Context, tool string) error { return errors.New("nope " + tool) }
	_, err := runDumpWithOrder(context.Background(), []string{"il2cppinspector", "il2cppdumper"}, run)
	if err == nil {
		t.Fatal("expected aggregate error when all dumpers fail")
	}
	if !strContains(err.Error(), "il2cppinspector") || !strContains(err.Error(), "il2cppdumper") {
		t.Fatalf("aggregate error must name both tools: %v", err)
	}
}

func strContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return len(sub) == 0
}
