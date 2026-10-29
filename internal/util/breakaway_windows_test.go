//go:build windows

package util

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Cancelling a breakaway step must kill the grandchild too: cmd.exe -> ping.exe
// stands in for analyzeHeadless.bat -> java.exe. Without the kill-on-close job
// ping survives the cmd.exe kill and keeps stdout open for ~60 s.
func TestBreakawayCancelKillsGrandchildren(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, 1)
	go func() {
		time.Sleep(1500 * time.Millisecond)
		cancel()
		started <- struct{}{}
	}()
	begin := time.Now()
	_, err := RunCmdStreamingEnvBreakaway(ctx, nil, "cmd.exe", []string{"/c", "ping -n 60 127.0.0.1"}, "", nil)
	if err != nil && strings.Contains(err.Error(), "start ") {
		t.Skipf("breakaway spawn not permitted here: %v", err)
	}
	<-started
	if d := time.Since(begin); d > 20*time.Second {
		t.Fatalf("cancel took %v: grandchild survived and held the pipe", d)
	}
}
