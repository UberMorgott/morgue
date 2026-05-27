package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/UberMorgott/morgue/internal/recipe"
	"github.com/UberMorgott/morgue/internal/recon"
	"github.com/UberMorgott/morgue/internal/services"
)

// --- Pipeline handlers ---

type runRequest struct {
	Path   string `json:"path"`
	Output string `json:"output"`
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	var req runRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}

	// Direct execution: run pipeline in a background goroutine.
	// Progress is available via GET /api/run/status and SSE /api/events.
	// Opt-in via ?direct=true — used by CLI --wait to poll completion.
	if r.URL.Query().Get("direct") == "true" {
		go func() {
			if err := s.pipeline.Run(req.Path, req.Output); err != nil {
				s.events.Broadcast("pipeline:error", marshalJSON(map[string]string{
					"error": err.Error(),
				}))
			}
		}()
		writeJSON(w, http.StatusOK, map[string]string{"status": "started"})
		return
	}

	// Default: push to command queue for frontend polling (GUI-coordinated runs).
	// Direct execution from HTTP goroutines emits Wails events that don't reach the
	// webview, so the frontend must pick these up and execute via Wails bindings.
	s.tools.PushAPICommand(services.APICommand{
		Action: "run",
		Path:   req.Path,
		Output: req.Output,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "queued"})
}

func (s *Server) handleGetPipelineStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.pipeline.GetStatus())
}

// --- Tools handlers ---

func (s *Server) handleGetTools(w http.ResponseWriter, r *http.Request) {
	waitStr := r.URL.Query().Get("wait")
	if waitStr != "" {
		waitSec, err := strconv.Atoi(waitStr)
		if err != nil || waitSec < 1 {
			waitSec = 30
		}
		if waitSec > 120 {
			waitSec = 120
		}
		changed := s.tools.WaitForChange(time.Duration(waitSec) * time.Second)
		resp := s.tools.GetToolsEnriched()
		resp.Changed = changed
		writeJSON(w, http.StatusOK, resp)
		return
	}
	writeJSON(w, http.StatusOK, s.tools.GetToolsEnriched())
}

type toolRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleInstallTool(w http.ResponseWriter, r *http.Request) {
	var req toolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	// Push command to the queue for the frontend to pick up and execute through
	// Wails bindings. This ensures progress events are emitted in the Wails
	// context and reliably reach the webview.
	if req.Name == "" {
		s.tools.PushAPICommand(services.APICommand{Action: "install-all"})
		s.events.Broadcast("tool:install:start", marshalJSON(map[string]string{"tool": "all"}))
		writeJSON(w, http.StatusOK, map[string]string{"status": "installing all"})
		return
	}

	s.tools.PushAPICommand(services.APICommand{Action: "install", Tool: req.Name})
	s.events.Broadcast("tool:install:start", marshalJSON(map[string]string{"tool": req.Name}))
	writeJSON(w, http.StatusOK, map[string]string{"status": "installing", "name": req.Name})
}

func (s *Server) handleDeleteTool(w http.ResponseWriter, r *http.Request) {
	var req toolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	// Push command to the queue for the frontend to pick up and execute through
	// Wails bindings, keeping it consistent with the install path.
	s.tools.PushAPICommand(services.APICommand{Action: "delete", Tool: req.Name})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleting", "name": req.Name})
}

// --- Settings handlers ---

func (s *Server) handleGetSettings(w http.ResponseWriter, _ *http.Request) {
	cfg, err := s.config.Get()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	// Redact sensitive fields before returning.
	redacted := *cfg
	if redacted.GitHubToken != "" {
		redacted.GitHubToken = "***"
	}
	writeJSON(w, http.StatusOK, &redacted)
}

type updateSettingsRequest struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

// settingsAllowlist defines which config keys may be modified via the API.
var settingsAllowlist = map[string]bool{
	"MaxFileSizeMB":          true,
	"SkipSystemLibs":         true,
	"GeneratePDB":            true,
	"AutoUpdateCheck":        true,
	"DefaultOutputDir":       true,
	"UE5GhidraDecompile":    true,
	"AllowDynamicExecution":  true,
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req updateSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "key is required"})
		return
	}

	if !settingsAllowlist[req.Key] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "setting not modifiable via API"})
		return
	}

	cfg, err := s.config.Get()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	// Marshal config to a map, update the key, unmarshal back.
	data, err := json.Marshal(cfg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	if _, ok := m[req.Key]; !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown setting key"})
		return
	}
	m[req.Key] = req.Value

	data, err = json.Marshal(m)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	if err := s.config.Save(*cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	// Redact sensitive fields before returning.
	if cfg.GitHubToken != "" {
		cfg.GitHubToken = "***"
	}
	writeJSON(w, http.StatusOK, cfg)
}

// marshalJSON is a convenience wrapper around json.Marshal that swallows errors.
// It is safe for fire-and-forget SSE payloads where encoding cannot realistically fail.
func marshalJSON(v any) []byte {
	data, _ := json.Marshal(v)
	return data
}

// writeJSON is a helper that encodes v as JSON and writes it to w.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// --- Info handler ---

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path query parameter is required"})
		return
	}

	if _, err := os.Stat(path); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
		return
	}

	result, err := recon.Classify(context.Background(), path)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	recipeName := ""
	if rec := recipe.Match(&result); rec != nil {
		recipeName = rec.Name()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"path":                path,
		"size":                result.Size,
		"sha256":              result.SHA256,
		"kind":                result.Kind.String(),
		"runtime":             result.Runtime,
		"compiler":            result.Compiler,
		"obfuscator":          result.Obfuscator,
		"obfuscator_features": result.ObfuscatorFeatures,
		"packed":              result.Packed,
		"embedded_suspected":  result.EmbeddedSuspected,
		"embedded_signals":    result.EmbeddedSignals,
		"recipe":              recipeName,
	})
}
