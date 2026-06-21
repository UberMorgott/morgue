package recipe

import (
	"strings"
	"testing"
)

func TestRipperLaunchArgs(t *testing.T) {
	args := ripperLaunchArgs(45678)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--headless") {
		t.Fatalf("args missing --headless: %q", joined)
	}
	if !strings.Contains(joined, "--port 45678") {
		t.Fatalf("args missing --port: %q", joined)
	}
}

func TestRipperEnv(t *testing.T) {
	env := ripperEnv("D:/out/.tmp")
	if !hasEnv(env, "TEMP", "D:/out/.tmp") || !hasEnv(env, "TMP", "D:/out/.tmp") {
		t.Fatalf("ripper env missing TEMP/TMP: %v", env)
	}
}
