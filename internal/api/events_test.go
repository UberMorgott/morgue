package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSSEBroadcast(t *testing.T) {
	eb := NewEventBroadcaster()

	srv := httptest.NewServer(http.HandlerFunc(eb.HandleSSE))
	defer srv.Close()

	go func() {
		time.Sleep(100 * time.Millisecond)
		eb.Broadcast("test:event", []byte(`{"msg":"hello"}`))
		time.Sleep(50 * time.Millisecond)
		eb.Shutdown()
	}()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	output := string(body)

	if !strings.Contains(output, "event: test:event") {
		t.Errorf("expected event line, got: %s", output)
	}
	if !strings.Contains(output, `data: {"msg":"hello"}`) {
		t.Errorf("expected data line, got: %s", output)
	}
}
