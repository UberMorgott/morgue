//go:build windows

package main

import (
	"bytes"
	"io"
	"os"
	"testing"
)

// TestAttachParentConsoleKeepsRedirectedStdout guards the v0.4.5 CLI-stdout
// regression: when stdout is already a valid handle (here a pipe, standing in
// for a redirect to a file by Start-Process/`>`), the console-attach shim must
// no-op and leave os.Stdout intact instead of rebinding it to the terminal's
// CONOUT$. We point os.Stdout at a pipe, call the shim, write, and assert the
// bytes land in the pipe — i.e. a redirected `morgue run --quiet > out.json`
// still captures its JSON.
func TestAttachParentConsoleKeepsRedirectedStdout(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	attachParentConsole()

	const msg = "stdout-redirect-ok"
	if _, err := os.Stdout.Write([]byte(msg)); err != nil {
		os.Stdout = orig
		t.Fatalf("write to redirected stdout failed after attachParentConsole: %v", err)
	}
	w.Close()
	os.Stdout = orig

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(msg)) {
		t.Fatalf("attachParentConsole clobbered redirected stdout: got %q", buf.String())
	}
}
