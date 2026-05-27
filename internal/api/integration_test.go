package api

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/UberMorgott/morgue/internal/services"
)

func TestIntegrationFullWorkflow(t *testing.T) {
	pipeline := services.NewPipelineService()
	tools := services.NewToolsService("test")
	cfg := &services.ConfigService{}
	recon := &services.ReconService{}

	srv := NewServer(pipeline, tools, cfg, recon)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer srv.Stop()

	time.Sleep(200 * time.Millisecond)

	base := "http://127.0.0.1:19876"

	// 1. GET /api/status — 200, JSON with running=true
	t.Run("GET /api/status", func(t *testing.T) {
		resp, err := http.Get(base + "/api/status")
		if err != nil {
			t.Fatalf("request error: %v", err)
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
	})

	// 2. GET /api/tools — 200, non-empty JSON array
	t.Run("GET /api/tools", func(t *testing.T) {
		resp, err := http.Get(base + "/api/tools")
		if err != nil {
			t.Fatalf("request error: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode error: %v", err)
		}
		tools, ok := result["tools"].([]any)
		if !ok || len(tools) == 0 {
			t.Fatal("expected non-empty tools array in response")
		}
	})

	// 3. GET /api/settings — 200, JSON object
	t.Run("GET /api/settings", func(t *testing.T) {
		resp, err := http.Get(base + "/api/settings")
		if err != nil {
			t.Fatalf("request error: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode error: %v", err)
		}
	})

	// 4. GET /api/instructions — 200, non-empty text
	t.Run("GET /api/instructions", func(t *testing.T) {
		resp, err := http.Get(base + "/api/instructions")
		if err != nil {
			t.Fatalf("request error: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		data, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body error: %v", err)
		}
		if len(data) == 0 {
			t.Fatal("expected non-empty instructions body")
		}
	})

	// 5. GET /api/run/status — 200, JSON object
	t.Run("GET /api/run/status", func(t *testing.T) {
		resp, err := http.Get(base + "/api/run/status")
		if err != nil {
			t.Fatalf("request error: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode error: %v", err)
		}
	})
}
