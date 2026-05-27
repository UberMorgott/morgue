package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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

	req := httptest.NewRequest(http.MethodGet, "/api/tools", nil)
	rec := httptest.NewRecorder()

	s.handleGetTools(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %s", ct)
	}

	var tools []any
	if err := json.NewDecoder(rec.Body).Decode(&tools); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	// CheckAll should return a non-nil slice.
	if tools == nil {
		t.Fatal("expected non-nil tools array")
	}
}

func TestHandleGetSettings(t *testing.T) {
	s := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
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

	body, _ := json.Marshal(map[string]string{
		"path":   "test.exe",
		"output": "out",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/run", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.handleRun(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
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
	req := httptest.NewRequest(http.MethodPost, "/api/run", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	s.handleRun(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleGetPipelineStatus(t *testing.T) {
	s := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/run/status", nil)
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
