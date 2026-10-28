package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/UberMorgott/morgue/internal/services"
)

// newTestServer creates a Server suitable for handler unit tests.
func newTestServer() *Server {
	pipeline := services.NewPipelineService()
	tools := services.NewToolsService("")
	cfg := &services.ConfigService{}
	recon := &services.ReconService{}
	return NewServer(pipeline, tools, cfg, recon)
}

func TestHandleGetTools(t *testing.T) {
	s := newTestServer()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/tools", nil)
	rec := httptest.NewRecorder()

	s.handleGetTools(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %s", ct)
	}

	var result map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	tools, ok := result["tools"].([]any)
	if !ok || tools == nil {
		t.Fatal("expected non-nil tools array in response")
	}
}

func TestHandleGetSettings(t *testing.T) {
	s := newTestServer()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/settings", nil)
	rec := httptest.NewRecorder()

	s.handleGetSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var cfg map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	// Config should have some known fields.
	if _, ok := cfg["SkipSystemLibs"]; !ok {
		t.Fatal("expected SkipSystemLibs in config")
	}
}

func TestHandleRunDecompilation(t *testing.T) {
	s := newTestServer()
	// The run executes in the background; wait for it to finish before the
	// test returns so it never writes into a TempDir that is being removed.
	finished := make(chan struct{})
	services.SetRunFinishedHook(s.pipeline, func(error) { close(finished) })

	// Admission validates the input now, so the target must exist.
	in := filepath.Join(t.TempDir(), "test.exe")
	if err := os.WriteFile(in, []byte("MZ"), 0o644); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{
		"path":   in,
		"output": filepath.Join(t.TempDir(), "out"),
	})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/run", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.handleRun(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rec.Code)
	}
	if err := s.pipeline.StopActive(); err != nil && !errors.Is(err, services.ErrNotRunning) {
		t.Fatalf("stop: %v", err)
	}
	select {
	case <-finished:
	case <-time.After(30 * time.Second):
		t.Fatal("background run did not finish after stop")
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp["status"] != "started" {
		t.Fatalf("expected status=started, got %s", resp["status"])
	}
}

func TestHandleRunMissingPath(t *testing.T) {
	s := newTestServer()

	body, _ := json.Marshal(map[string]string{"output": "out"})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/run", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	s.handleRun(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleGetPipelineStatus(t *testing.T) {
	s := newTestServer()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/run/status", nil)
	rec := httptest.NewRecorder()

	s.handleGetPipelineStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var status map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&status); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if status["running"] != false {
		t.Fatalf("expected running=false, got %v", status["running"])
	}
}
