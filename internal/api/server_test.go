package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/UberMorgott/morgue/internal/services"
)

func TestServerStartsAndStops(t *testing.T) {
	pipeline := services.NewPipelineService()
	tools := services.NewToolsService("")
	cfg := &services.ConfigService{}
	recon := &services.ReconService{}

	srv := NewServer(pipeline, tools, cfg, recon)

	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Give the server a moment to start listening.
	time.Sleep(50 * time.Millisecond)

	resp, err := http.Get("http://127.0.0.1:19876/api/status")
	if err != nil {
		t.Fatalf("GET /api/status error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if body["running"] != true {
		t.Fatalf("expected running=true, got %v", body["running"])
	}
	if body["port"] != "19876" {
		t.Fatalf("expected port=19876, got %v", body["port"])
	}

	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	// Verify server is unreachable after stop.
	_, err = http.Get("http://127.0.0.1:19876/api/status")
	if err == nil {
		t.Fatal("expected error after stop, got nil")
	}
}
